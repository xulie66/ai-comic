package generation

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Static data copied from the application's existing OpenCC dictionaries; no runtime JS dependency.
//
//go:embed TSCharacters.txt
var charactersData string

//go:embed TSPhrases.txt
var phrasesData string

//go:embed layouts.json
var layoutsData []byte

type conversionNode struct {
	next  map[rune]*conversionNode
	value string
}

var converter = buildConverter()

func buildConverter() *conversionNode {
	root := &conversionNode{}
	for _, data := range []string{charactersData, phrasesData} {
		for _, entry := range strings.Split(strings.TrimPrefix(data, "\ufeff"), "|") {
			parts := strings.Fields(entry)
			if len(parts) < 2 {
				continue
			}
			n := root
			for _, r := range parts[0] {
				if n.next == nil {
					n.next = map[rune]*conversionNode{}
				}
				if n.next[r] == nil {
					n.next[r] = &conversionNode{}
				}
				n = n.next[r]
			}
			n.value = parts[1]
		}
	}
	return root
}
func simplified(text string) string {
	r := []rune(text)
	var out strings.Builder
	for i := 0; i < len(r); {
		if r[i] == '么' {
			out.WriteRune(r[i])
			i++
			continue
		}
		n := converter
		value := ""
		end := i
		for j := i; j < len(r); j++ {
			n = n.next[r[j]]
			if n == nil {
				break
			}
			if n.value != "" {
				value = n.value
				end = j + 1
			}
		}
		if value != "" {
			out.WriteString(value)
			i = end
		} else {
			out.WriteRune(r[i])
			i++
		}
	}
	return out.String()
}

const scriptRule = "可见文字语言：zh-CN，中国大陆规范简体中文。字体采用思源黑体简体版（Noto Sans CJK SC）或微软雅黑，横排、完整笔画。画风仅作用于人物和背景，汉字保持现代简体印刷字形。参考图只参考造型与空间，不参考其文字。画面中只绘制指定文案，逐字照录，不翻译、不润色、不增加拟声字、招牌或说明。先给文案预留白底文字区域，再构图；长句换行或扩大文字区域。最终检查每个文字框的笔画和文案，修正非简体字形后输出图片。"

var scriptGuardRE = regexp.MustCompile(`【ZH_CN_TEXT_V2】[\s\S]*?【/ZH_CN_TEXT_V2】\s*`)

const finalScriptCheck = "【最终文字排版检查】全部可见中文使用 zh-CN 简体规范印刷字。逐字核对本次文案，保持原句与标点，检查后输出图片，不输出检查说明。"

func enforceSimplified(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(scriptGuardRE.ReplaceAllString(s, ""), finalScriptCheck, ""))
	return "【ZH_CN_TEXT_V2】\n" + scriptRule + "\n【/ZH_CN_TEXT_V2】\n\n" + simplified(s) + "\n\n" + finalScriptCheck
}
func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}
func list(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	if a, ok := v.([]map[string]any); ok {
		r := make([]any, len(a))
		for i := range a {
			r[i] = a[i]
		}
		return r
	}
	if a, ok := v.([]string); ok {
		r := make([]any, len(a))
		for i := range a {
			r[i] = a[i]
		}
		return r
	}
	return nil
}
func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprint(v)
}
func clean(v any) string { return simplified(strings.Join(strings.Fields(str(v)), " ")) }
func first(m map[string]any, keys ...string) any {
	for _, k := range keys {
		if v := m[k]; v != nil && str(v) != "" {
			return v
		}
	}
	return nil
}
func fallback(v any, s string) string {
	if t := clean(v); t != "" {
		return t
	}
	return s
}
func jsonText(v any) string { b, _ := json.MarshalIndent(v, "", "  "); return simplified(string(b)) }
func textList(v any) []string {
	out := []string{}
	if a := list(v); a != nil {
		for _, item := range a {
			out = append(out, textList(item)...)
		}
		return out
	}
	if m, ok := v.(map[string]any); ok {
		return textList(first(m, "text", "content", "dialogue", "dialog"))
	}
	for _, s := range strings.Split(str(v), "\n") {
		if s = clean(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
func joined(v any) string { return strings.Join(textList(v), "、") }

type layout struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	AspectRatios []string `json:"aspectRatios"`
	PanelCount   int      `json:"panelCount"`
}

func layouts(aspect string) []layout {
	var all []layout
	_ = json.Unmarshal(layoutsData, &all)
	out := []layout{}
	for _, l := range all {
		for _, a := range l.AspectRatios {
			if a == aspect {
				out = append(out, l)
				break
			}
		}
	}
	return out
}
func layoutFor(id, aspect string, count int) layout {
	aliases := map[string]string{"single_large": "full_page_large", "three_stack": "vertical_three", "big_plus_closeup": "panorama_plus_closeup", "diagonal_conflict": "diagonal_split", "单大图": "full_page_large", "上大下双": "top_large_bottom_two", "三格递进": "vertical_three"}
	if a := aliases[id]; a != "" {
		id = a
	}
	options := layouts(aspect)
	for _, l := range options {
		if l.ID == id {
			return l
		}
	}
	for _, l := range options {
		if l.PanelCount == count {
			return l
		}
	}
	if len(options) > 0 {
		return options[0]
	}
	return layout{ID: "full_page_large", Name: "单大图", Description: "单格完整构图"}
}
func style(p map[string]any) string {
	styles := map[string]string{"guoman": "高颜值国漫：干净描边、赛璐璐上色、细腻表情，影视感光影。", "chibi": "Q萌二头身：大头小身，头身约1:1到1:2，短圆四肢，明亮配色；禁止写实比例和皮肤质感。", "stick": "简笔小人：圆头、点眼和单线嘴，服装配色、配饰和身高差区分角色；无写实五官、复杂背景或材质。", "matchstick": "火柴人：空心圆头、统一粗细单线躯干四肢，只靠线条颜色、身高差和一个最小标记辨认；纯白背景，禁止服装款式、写实五官、阴影渐变。", "american": "美式卡通：统一粗黑描边、纯色平涂、几何化造型、夸张表情；禁止写实、渐变、厚涂和纹理。", "mangabw": "日式黑白漫画原稿：黑白墨线、网点灰阶、高反差，只能黑白灰，禁止任何彩色和照片级灰度渲染。", "clay": "黏土玩偶：手工黏土材质和轻微指痕、圆润三到四头身，统一柔和布光；禁止真人和2D平涂。"}
	if s := styles[str(p["artStyle"])]; s != "" {
		return s
	}
	return styles["guoman"]
}
func referenceBindings(p map[string]any) string {
	out := []string{}
	for i, r := range list(p["references"]) {
		if i >= 16 {
			break
		}
		m := obj(r)
		kind := map[string]string{"characters": "角色", "scenes": "场景", "props": "道具"}[str(m["type"])]
		if kind == "" {
			kind = "角色"
		}
		out = append(out, fmt.Sprintf("参考图%d = %s %s", i+1, kind, clean(m["name"])))
	}
	if len(out) == 0 {
		return "无参考图"
	}
	return strings.Join(out, "；")
}
func locks(p map[string]any) string {
	var b strings.Builder
	for _, item := range []struct{ k, label string }{{"pageCharacterLock", "本页人物一致性锁定（最高优先级）"}, {"pageSceneLock", "本页场景一致性锁定（最高优先级）"}} {
		if v := clean(p[item.k]); v != "" {
			fmt.Fprintf(&b, "\n%s：%s。不得被自定义构图覆盖。\n", item.label, v)
		}
	}
	return b.String()
}

func StoryboardPrompt(p map[string]any) string {
	aspect := fallback(p["aspectRatio"], "9:16")
	var catalog strings.Builder
	for _, l := range layouts(aspect) {
		fmt.Fprintf(&catalog, "- %s（%s）：%d格；%s\n", l.ID, l.Name, l.PanelCount, l.Description)
	}
	pageRule := "用户未指定目标页数：按剧情节奏决定，不得空页。"
	if clean(p["targetPageCount"]) != "" && clean(p["targetPageCount"]) != "0" {
		pageRule = "目标漫画页数：" + clean(p["targetPageCount"]) + "；优先保持原文顺序与完整剧情，再贴近目标页数。"
	}
	libraryRule := "只输出对剧情有连续作用的明确人物，不提取路人或泛称群体；角色字段为 name,role,gender,age,identity,personality,appearance,outfit,props,importance,inferenceNotes。role 仅为 male_lead/female_lead/supporting/antagonist/elder/child。姓名必须是唯一人物称呼，不得使用动作、原文句子或代词。性别依据明确称谓而非职业。缺失外貌填未明确描述，合理视觉补全写入 inferenceNotes。"
	if chars := list(p["canonicalCharacters"]); len(chars) > 0 {
		libraryRule = "全书固定角色档案（权威，不得改写）：\n" + jsonText(chars) + "\n严格沿用姓名、身份、性别、年龄、外貌、服装与辨识特征；不得重设计角色；characterLibrary 输出空数组。"
	} else if p["extractCharacters"] == false || p["extractCharactersWithStoryboard"] == false {
		libraryRule = "不要提取角色库；characterLibrary 输出空数组。"
	}
	schema := `{"chunkIndex":1,"title":"标题","style":"画风","panel_count_reason":"分页理由","maleLead":{},"femaleLead":{},"supportingCharacters":[],"characterLibrary":[],"sceneCard":{"name":"","styleDescription":"","mood":""},"pages":[{"page_number":1,"page_core":"核心剧情","original_text_range":"顺序使用的完整原文","page_function":"普通推进页","layout_type":"模板id","panel_count":1,"panels":[{"panel_number":1,"panel_size":"大格","shot_type":"face_closeup","scene":"具体地点","characters":[],"action":"可见动作","emotion":"自然","title_text":"","narration_text":"","dialogue_text":[],"speaker":"","bubblePosition":"auto","phone_text":[],"visual_prompt":"可见人物环境动作镜头构图","text_position":"顶部左侧"}],"page_image_prompt":"整页构图、分格关系与文字框"}]}`
	return simplified(fmt.Sprintf(`你是中文漫画分镜编辑。将【当前正文】按顺序转换为漫画页 -> 分镜格 -> 画面文字 -> 生图提示词的严格 JSON，只返回 JSON，不要 Markdown。
不新增剧情、不改变人物关系、不删关键剧情，不压缩为短剧大纲。每页一个核心剧情点，每格一个最小信息点。每页1到4格；手机信息使用 phone_text 与 phone_closeup；误会反转独立成格并优先页尾；心理独白保持旁白，不硬改动作；关键道具使用 object_closeup。
%s
页面比例：%s。layout_type 仅从以下适配模板选择：
%s
scene 必须是具体地点，同地点的机位、表情、对白变化不改变场景。shot_type 仅可为 establish_scene、action_mid_shot、phone_closeup、face_closeup、eye_closeup、object_closeup、split_screen、wide_shot、over_shoulder、top_down。
文字规则：title_text 仅用于首页钩子或页面核心，12到24字；narration_text 只放原文叙述、心理、时间、环境与动作说明，每行8到22字；dialogue_text 只放真实说出的台词，保留原意并填写 speaker；phone_text 保留手机界面原文。引号、冒号后及明显说出口的无引号台词均进入对白。同一句禁止同时进入旁白和对白。纯氛围格允许所有文字为空。旁白与标题严禁镜头术语、英文镜头枚举、构图描述和 visual_prompt 内容。
%s
%s
统一画风：%s
角色与场景卡：%s
【前文上下文，仅供理解，不生成】：%s
【当前正文，必须完整处理】：
%s
【后文上下文，仅供理解，不生成】：%s
必须使用结构：%s`, pageRule, aspect, catalog.String(), scriptRule, libraryRule, style(p), jsonText(map[string]any{"maleLead": p["maleLead"], "femaleLead": p["femaleLead"], "supportingCharacters": p["supportingCharacters"], "sceneCard": p["sceneCard"], "assetLibrary": p["assetLibrary"]}), fallback(p["contextBefore"], "无"), str(first(p, "originalText", "text")), fallback(p["contextAfter"], "无"), schema))
}

func PagePrompt(p map[string]any) string {
	page := obj(p["page"])
	panels := list(page["panels"])
	aspect := fallback(p["aspectRatio"], "9:16")
	l := layoutFor(clean(page["layout_type"]), aspect, len(panels))
	var detail strings.Builder
	texts := []map[string]any{}
	for i, item := range panels {
		panel := obj(item)
		index := first(panel, "panel_index", "panel_number")
		if index == nil {
			index = i + 1
		}
		fmt.Fprintf(&detail, "\n分镜 %v：\n- 格子尺寸：%s\n- 镜头：%s\n- 人物：%s\n- 场景：%s\n- 情绪：%s\n- 动作：%s\n- 画面提示：%s\n- 说话人：%s\n- 文字位置：%s\n- 可见文字：仅按最终上字清单绘制\n", index, fallback(panel["panel_size"], "中格"), fallback(first(panel, "shot_type", "camera"), "中景"), fallback(joined(panel["characters"]), "按剧情需要"), fallback(first(panel, "scene", "background"), fallback(page["scene"], "当前剧情场景")), fallback(panel["emotion"], "自然"), fallback(panel["action"], "自然姿态"), fallback(first(panel, "visual_prompt", "image_prompt", "imagePrompt"), "按镜头动作情绪构图"), fallback(panel["speaker"], "按剧情人物判断"), fallback(first(panel, "text_position", "bubblePosition"), "画面空白处"))
		texts = append(texts, map[string]any{"分镜": index, "标题": textList(panel["title_text"]), "旁白": textList(first(panel, "narration", "narration_text")), "对白": textList(first(panel, "dialog", "dialogue_text", "dialogue")), "手机文字": textList(panel["phone_text"])})
	}
	override := first(page, "promptOverride")
	if override == nil {
		override = p["promptOverride"]
	}
	custom := ""
	if clean(override) != "" {
		custom = "【自定义构图补充】\n" + clean(override) + "\n自定义内容仅用于构图和画面，必须保持以下人物、场景锁定与简体文案清单。"
	}
	sb := obj(p["storyboard"])
	return enforceSimplified(fmt.Sprintf(`生成一张带完整简体中文文字的漫画成品页。整页一次生成，画面与文字在同一张最终图片中完成，排好 %d 个分镜格子。
画幅比例：%s
分辨率：%s
图片质量：%s
页面布局：%s（%s）
布局说明：%s
仅使用适配当前 %s 比例的模板，不得错位或留大面积空白。
页面场景：%s
统一画风：%s
参考图绑定：%s
参考图只锁定对应角色脸型、五官、发型、发色、年龄、服装；不得互换男主女主；不照搬角色参考背景；场景和道具参考只作用于对应对象。简笔小人与火柴人只按其极简画风，以配色、身高和标记辨认。
%s
%s
男主角色卡：%s
女主角色卡：%s
配角角色卡：%s
场景卡：%s
文字与画面硬性要求：
1. 严格逐字绘制每格标题、旁白、对白和手机文字，禁止漏字、改写、重复和额外文字。只使用简体中文。
2. 标题和旁白用白底黑字黑边矩形框；对白用白底黑字气泡，尾巴指向说话人；手机文字放入清晰消息界面。
3. 中文字体清晰端正，自动换行，文字框避开脸部、眼睛、关键动作与道具；禁止乱码、伪文字、英文、水印和无关UI。
4. 同一空间的墙、门窗、家具朝向、材质、光源方向与色温完全一致；只改变机位、景别与人物动作，不得增删家具或改造空间。
5. 角色脸型、发型、发色、年龄、服装与身高比例全页连续，按明确年龄绘制，不擅自改变性别与辨识特征。
本页分镜：%s
【逐格最终上字清单：只绘制清单文案，不绘制字段名称；空项不画字】
%s
%s
清单文字逐项绘制一次；画面描述、角色资料和规则说明不作为可见文字。`, len(panels), aspect, fallback(p["quality"], "标准"), fallback(p["imageQuality"], "auto"), l.ID, l.Name, l.Description, aspect, fallback(page["scene"], "当前剧情场景"), style(p), referenceBindings(p), custom, locks(p), jsonText(sb["maleLead"]), jsonText(sb["femaleLead"]), jsonText(sb["supportingCharacters"]), jsonText(sb["sceneCard"]), detail.String(), jsonText(texts), bookKeyword(obj(p["bookKeyword"]), page)))
}
func number(v any, def int) int {
	n, e := strconv.Atoi(str(v))
	if e != nil {
		if f, ok := v.(float64); ok {
			n = int(f)
		} else {
			return def
		}
	}
	if n < 1 {
		n = 1
	}
	if n > 200 {
		n = 200
	}
	return n
}
func bookKeyword(config, page map[string]any) string {
	if config["enabled"] != true {
		return "书名关键词：未启用。不要生成任何书名关键词、页码、角标、签名、水印或页面编号。"
	}
	parts := []string{}
	keyword := strings.Trim(clean(config["keyword"]), "《》")
	if utf8.RuneCountInString(keyword) > 10 {
		keyword = string([]rune(keyword)[:10])
	}
	if config["showKeyword"] != false && keyword != "" {
		parts = append(parts, "《"+keyword+"》")
	}
	if config["showNumber"] != false {
		n := number(first(page, "page_index", "page_number", "pageIndex"), 1)
		if config["numberMode"] == "manual" {
			n = number(config["manualNumber"], 1)
		}
		parts = append(parts, strconv.Itoa(n))
	}
	if len(parts) == 0 {
		return "无页面角标。"
	}
	position := map[string]string{"top_left": "左上角", "top_right": "右上角", "bottom_left": "左下角", "bottom_right": "右下角"}[str(config["position"])]
	if position == "" {
		position = "右下角"
	}
	return fmt.Sprintf("页面角标文案：%s。仅在%s用白底黑字细黑框小字号生成，逐字照录，不遮挡人物、剧情文字与关键道具。", strings.Join(parts, " "), position)
}

func CharacterPrompt(p map[string]any) string {
	ch := obj(p["character"])
	if len(ch) == 0 {
		ch = p
	}
	override := first(ch, "promptOverride")
	if override == nil {
		override = p["promptOverride"]
	}
	return enforceSimplified(fmt.Sprintf(`生成横向16:9人物设定表（CHARACTER SHEET），单一特定角色的唯一视觉圣经，不是剧情插画、海报或多人场景。
角色资料（权威）：%s
统一画风：%s
自定义构图补充：%s
【人物身份锁定，优先于自定义补充】所有视图和表情必须是同一人物、同一脸型五官比例、同一明确性别、年龄、身材比例、发型发色、服装、配饰与辨识特征；只改变机位与表情。职业、气质、短发、西装等不得推翻资料中的明确性别。未知性别或年龄保持资料，不编造。角色外貌、服装、道具按资料落实，缺失属性保持未明确描述，禁止套用统一美女帅哥模板。
固定版式：白色或中性浅灰背景，专业边框和分区线，左侧约60%%展示全身正面、四分之三、侧面、背面四视图；右上六个头像表情：自然、开心、认真、悲伤、生气、惊讶；右下当前角色的脸部、头发、服装、鞋子、配饰、道具和特殊标记细节。简笔与火柴人仅画其允许的轮廓、配色或标记，黑白漫画禁止彩色。
角色姓名“%s”与视角标签仅为辅助，保持人物主体足够大；禁止复杂背景、建筑、剧情动作、第二个人物、气泡、水印和大段文字。
最终再次锁定人物资料：%s`, jsonText(ch), style(p), fallback(override, "无"), fallback(ch["name"], "角色"), jsonText(ch)))
}
func ScenePrompt(p map[string]any) string {
	scene := obj(p["scene"])
	if len(scene) == 0 {
		scene = p
	}
	override := first(scene, "promptOverride")
	if override == nil {
		override = p["promptOverride"]
	}
	return enforceSimplified(fmt.Sprintf(`生成横向16:9场景设定图（SCENE SHEET），一个特定场景的唯一视觉基准图，不是漫画分镜、剧情插画或海报。
场景名称：%s
场景描述（权威）：%s
氛围：%s
时间/天气：%s
关键元素：%s
补充说明：%s
统一画风：%s
自定义构图补充：%s
【空间锁定，优先于自定义补充】严格保持资料中的墙体、门窗、家具位置朝向、通道、材质与光源方向，不增删关键元素，不改场景结构。
横版浅灰底，细黑分区线。左侧约62%%展示人眼高度的完整主视角全景，空间结构、进深和门窗家具关系清楚；右上38%%展示同一空间的俯视平面布局线稿，不写文字标注；右下两处关键细节特写，必须与主视角物件一致。所有部分属于同一个空间，家具朝向与光源方向一致。
硬性要求：画面绝对无人，无动物剪影、路人和镜中人影；不得出现任何文字、字母、数字、招牌、字幕、气泡、分镜格线、水印、logo；不混搭画风，不画鱼眼或超广角畸变。只交代环境，不表现剧情动作。
最终空间基准锁定：%s`, fallback(scene["name"], "主要场景"), fallback(scene["description"], "按剧情需要设置的空间"), fallback(scene["mood"], "自然"), fallback(first(scene, "time_of_day", "timeOfDay"), "按剧情需要"), fallback(joined(first(scene, "key_elements", "keyElements")), "按场景描述自然安排"), fallback(scene["notes"], "无"), style(p), fallback(override, "无"), jsonText(scene)))
}
