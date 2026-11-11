// 后端数据结构（只声明界面用得到的字段，后端多给的字段一律忽略）

export interface User {
  id: string
  username: string
  displayName?: string
  role?: string
  isAdmin?: boolean
  dailyPageQuota?: number
  createdAt?: string
  lastLoginAt?: string | null
  usage?: { storyboards: number; pages: number; images: number; characters: number }
}

export interface ProjectItem {
  id: string
  name: string
  root?: string
  absPath?: string
  legacy?: boolean
  pageCount?: number
  panelCount?: number
  characterCount?: number
  doneCount?: number
  cover?: string
  title?: string
  updatedAt?: string
  active?: boolean
}

export interface ProjectList {
  activeId: string
  activeRoot: string
  items: ProjectItem[]
}

export interface CharacterCard {
  name: string
  role?: string
  gender?: string
  age?: string
  identity?: string
  personality?: string
  appearance?: string
  outfit?: string
  props?: string
  promptOverride?: string
  referenceImagePath?: string
  status?: string
  error?: string
}

export interface SceneCard {
  name: string
  description?: string
  mood?: string
  time_of_day?: string
  key_elements?: string[]
  notes?: string
  refImagePath?: string
  promptOverride?: string
  status?: string
  count?: number
  aliases?: string[]
  error?: string
}

export interface Panel {
  panel_index?: number
  index?: number
  scene?: string
  scene_raw?: string
  action?: string
  visual_prompt?: string
  narration_text?: string
  dialogue_text?: string
  text?: string
  text_overlay?: string
  size?: string
  promptOverride?: string
  imagePath?: string
  status?: string
  [key: string]: any
}

export interface Page {
  page_index?: number
  index?: number
  scene?: string
  page_core?: string
  summary?: string
  layout?: string
  panels?: Panel[]
  imagePath?: string
  imagePrompt?: string
  promptOverride?: string
  status?: string
  [key: string]: any
}

export interface Storyboard {
  pages: Page[]
  characterLibrary?: CharacterCard[]
  sceneLibrary?: SceneCard[]
  maleLead?: CharacterCard
  femaleLead?: CharacterCard
  supportingCharacters?: CharacterCard[]
  [key: string]: any
}

export interface Project {
  title?: string
  originalText?: string
  pageCount?: number
  aspectRatio?: string
  artStyle?: string
  quality?: string
  imageQuality?: string
  storyboard?: Storyboard
  characterLibrary?: CharacterCard[]
  sceneLibrary?: SceneCard[]
  [key: string]: any
}

export interface Settings {
  textApiUrl?: string
  textApiKey?: string
  textModel?: string
  imageApiUrl?: string
  imageApiKey?: string
  imageModel?: string
  imageSupportsReferences?: boolean
  [key: string]: any
}

export interface StateResponse {
  settings: Settings
  project: Project
  projects?: ProjectList
  activeProjectId?: string
  version?: string
}

export interface GenResult {
  success?: boolean
  imagePath?: string
  referenceImagePath?: string
  imagePrompt?: string
  taskId?: string
  userMessage?: string
  error?: string
  [key: string]: any
}
