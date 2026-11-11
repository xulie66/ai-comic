package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strings"

	"comic.local/go-service/internal/store"
)

const archiveTotalLimit int64 = 128 << 20
const archiveFileLimit int64 = 32 << 20

type archiveEntry struct {
	Name string
	Data []byte
}
type archiveBundle struct {
	Entries []archiveEntry
	Counts  map[string]int
	Parts   []string
}

// archive must run after authentication, before any other body decoder.
func (s *Server) archive(w http.ResponseWriter, r *http.Request, u store.PublicUser, pid string) (bool, error) {
	switch r.URL.Path {
	case "/api/project/import":
		if r.Method != "POST" {
			return true, store.ErrInvalid
		}
		out, e := s.importArchive(w, r, u.ID)
		if e == nil {
			s.json(w, 200, out)
		}
		return true, e
	case "/api/project/export":
		if r.Method != "GET" && r.Method != "HEAD" {
			return true, store.ErrInvalid
		}
		state, e := s.readState(r.Context(), u.ID, pid)
		if e != nil {
			return true, e
		}
		b, e := json.Marshal(map[string]any{"format": "ai-comic-project", "version": 1, "project": state})
		if e != nil {
			return true, e
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="project.json"`)
		if r.Method != "HEAD" {
			_, e = w.Write(b)
		}
		return true, e
	case "/api/pages/zip", "/api/pages/zip/plan", "/api/pages/exportAll":
	default:
		return false, nil
	}
	if r.URL.Path == "/api/pages/exportAll" {
		if r.Method != "POST" {
			return true, store.ErrInvalid
		}
		p, e := s.body(w, r)
		if e != nil {
			return true, e
		}
		out, e := s.exportAllArchive(r.Context(), u.ID, pid, p)
		if e == nil {
			s.json(w, 200, out)
		}
		return true, e
	}
	if r.Method != "GET" && r.Method != "HEAD" {
		return true, store.ErrInvalid
	}
	bundle, e := s.buildArchive(r.Context(), u.ID, pid, r.URL.Query().Get("parts"))
	if e != nil {
		return true, e
	}
	if strings.HasSuffix(r.URL.Path, "/plan") {
		var n int
		for _, v := range bundle.Entries {
			n += len(v.Data)
		}
		s.json(w, 200, map[string]any{"success": true, "parts": bundle.Parts, "counts": bundle.Counts, "files": len(bundle.Entries), "bytes": n})
		return true, nil
	}
	// Build completely before writing headers so malformed/missing assets never
	// produce an HTTP 200 followed by a JSON error inside a damaged ZIP.
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, v := range bundle.Entries {
		f, err := z.Create(v.Name)
		if err != nil {
			return true, err
		}
		if _, err = f.Write(v.Data); err != nil {
			return true, err
		}
	}
	if e = z.Close(); e != nil {
		return true, e
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="comic.zip"`)
	w.Header().Set("Content-Length", fmt.Sprint(buf.Len()))
	if r.Method != "HEAD" {
		_, e = w.Write(buf.Bytes())
	}
	return true, e
}
func archiveParts(raw string) ([]string, error) {
	if raw == "" {
		raw = "pages,script,project"
	}
	out := []string{}
	seen := map[string]bool{}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		switch part {
		case "pages", "script", "refs", "project", "panels":
		default:
			return nil, store.ErrInvalid
		}
		if !seen[part] {
			out = append(out, part)
			seen[part] = true
		}
	}
	return out, nil
}
func imageExtension(b []byte) (string, error) {
	switch http.DetectContentType(b) {
	case "image/png":
		return ".png", nil
	case "image/jpeg":
		return ".jpg", nil
	case "image/webp":
		return ".webp", nil
	case "image/gif":
		return ".gif", nil
	}
	return "", store.ErrInvalid
}
func archiveImagePaths(v any) []string {
	out := []string{}
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			switch k {
			case "imagePath", "page_image_url", "pageImageUrl", "referenceImagePath", "refImagePath", "image_url", "imageUrl":
				if p, ok := item.(string); ok && p != "" {
					out = append(out, p)
				}
			}
			switch item.(type) {
			case map[string]any, []any:
				out = append(out, archiveImagePaths(item)...)
			}
		}
	case []any:
		for _, item := range x {
			out = append(out, archiveImagePaths(item)...)
		}
	}
	return out
}
func rewriteArchiveImages(v any, mapping map[string]string, missing *int) {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			switch k {
			case "imagePath", "page_image_url", "pageImageUrl", "referenceImagePath", "refImagePath", "image_url", "imageUrl":
				if old, ok := item.(string); ok && old != "" {
					if next, exists := mapping[old]; exists {
						x[k] = next
					} else if missing != nil {
						*missing++
						x[k] = ""
					}
				}
			}
			switch item.(type) {
			case map[string]any, []any:
				rewriteArchiveImages(item, mapping, missing)
			}
		}
	case []any:
		for _, item := range x {
			rewriteArchiveImages(item, mapping, missing)
		}
	}
}
func (s *Server) buildArchive(ctx context.Context, uid, pid, rawParts string) (archiveBundle, error) {
	out := archiveBundle{Counts: map[string]int{"pages": 0, "script": 0, "refs": 0, "panels": 0, "missingPages": 0, "missingRefs": 0, "missingPanels": 0}}
	var e error
	out.Parts, e = archiveParts(rawParts)
	if e != nil {
		return out, e
	}
	wanted := map[string]bool{}
	for _, part := range out.Parts {
		wanted[part] = true
	}
	state, e := s.readState(ctx, uid, pid)
	if e != nil {
		return out, e
	}
	mapping := map[string]string{}
	var total int64
	appendEntry := func(name string, b []byte) error {
		if int64(len(b)) > archiveFileLimit || total+int64(len(b)) > archiveTotalLimit || len(out.Entries) >= 10000 {
			return store.ErrInvalid
		}
		total += int64(len(b))
		out.Entries = append(out.Entries, archiveEntry{name, b})
		return nil
	}
	addImage := func(source, base, category string) error {
		if source == "" {
			return nil
		}
		if _, ok := mapping[source]; ok {
			return nil
		}
		b, err := s.Store.ReadImage(ctx, uid, pid, source)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				out.Counts["missing"+strings.ToUpper(category[:1])+category[1:]]++
				return nil
			}
			return err
		}
		ext, err := imageExtension(b)
		if err != nil {
			return err
		}
		name := base + ext
		if err = appendEntry(name, b); err != nil {
			return err
		}
		mapping[source] = name
		out.Counts[category]++
		return nil
	}
	pages := array(object(state["storyboard"])["pages"])
	for i, pv := range pages {
		page := object(pv)
		n, e := pageIndex(page)
		if e != nil {
			n = i + 1
		}
		if wanted["pages"] {
			if e = addImage(pageImage(page), fmt.Sprintf("comic_page_%03d", n), "pages"); e != nil {
				return out, e
			}
		}
		if wanted["script"] {
			b, err := json.MarshalIndent(page, "", "  ")
			if err != nil {
				return out, err
			}
			if err = appendEntry(fmt.Sprintf("script/page_%03d.txt", n), b); err != nil {
				return out, err
			}
			out.Counts["script"]++
		}
		if wanted["panels"] {
			for j, panel := range array(page["panels"]) {
				for k, file := range archiveImagePaths(panel) {
					if e = addImage(file, fmt.Sprintf("panels/page_%03d_panel_%03d_%03d", n, j+1, k+1), "panels"); e != nil {
						return out, e
					}
				}
			}
		}
	}
	if wanted["refs"] {
		i := 0
		for _, root := range []map[string]any{state, object(state["storyboard"]), object(state["canonical"])} {
			for _, key := range []string{"characterLibrary", "sceneLibrary", "maleLead", "femaleLead", "supportingCharacters"} {
				for _, file := range archiveImagePaths(root[key]) {
					i++
					if e = addImage(file, fmt.Sprintf("refs/ref_%03d", i), "refs"); e != nil {
						return out, e
					}
				}
			}
		}
	}
	if wanted["project"] {
		rewriteArchiveImages(state, mapping, nil)
		b, err := json.Marshal(state)
		if err != nil {
			return out, err
		}
		if err = appendEntry("project.json", b); err != nil {
			return out, err
		}
	}
	return out, nil
}
func (s *Server) exportAllArchive(ctx context.Context, uid, pid string, p map[string]any) (map[string]any, error) {
	pages := array(p["pages"])
	if pages == nil {
		state, e := s.readState(ctx, uid, pid)
		if e != nil {
			return nil, e
		}
		pages = array(object(state["storyboard"])["pages"])
	}
	files := []string{}
	for i, pv := range pages {
		page := object(pv)
		source := pageImage(page)
		if source == "" {
			continue
		}
		b, e := s.Store.ReadImage(ctx, uid, pid, source)
		if e != nil {
			return nil, e
		}
		ext, e := imageExtension(b)
		if e != nil {
			return nil, e
		}
		n, e := pageIndex(page)
		if e != nil {
			n = i + 1
		}
		file, e := s.Store.WriteImage(ctx, uid, pid, fmt.Sprintf("output/comic_page_%03d%s", n, ext), b)
		if e != nil {
			return nil, e
		}
		files = append(files, file)
	}
	return map[string]any{"success": true, "directory": "output", "files": files}, nil
}
func validArchiveName(name string) bool {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(name, "/"), "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}
func readArchiveZIP(b []byte) (map[string]any, map[string][]byte, error) {
	z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if e != nil || len(z.File) > 10000 {
		return nil, nil, store.ErrInvalid
	}
	files := map[string][]byte{}
	var total int64
	for _, f := range z.File {
		if !validArchiveName(f.Name) || f.Mode()&ioModeSymlink != 0 {
			return nil, nil, store.ErrInvalid
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if _, exists := files[f.Name]; exists {
			return nil, nil, store.ErrInvalid
		}
		if f.UncompressedSize64 > uint64(archiveFileLimit) {
			return nil, nil, store.ErrInvalid
		}
		rc, e := f.Open()
		if e != nil {
			return nil, nil, e
		}
		data, e := io.ReadAll(io.LimitReader(rc, archiveFileLimit+1))
		rc.Close()
		if e != nil {
			return nil, nil, e
		}
		total += int64(len(data))
		if int64(len(data)) > archiveFileLimit || total > archiveTotalLimit {
			return nil, nil, store.ErrInvalid
		}
		files[f.Name] = data
	}
	var state map[string]any
	if json.Unmarshal(files["project.json"], &state) != nil || state == nil {
		return nil, nil, store.ErrInvalid
	}
	return unwrapArchiveProject(state), files, nil
}

// os.FileMode's symlink bit, kept as a named constant for ZIP entry checks.
const ioModeSymlink = 1 << 27

func unwrapArchiveProject(p map[string]any) map[string]any {
	for i := 0; i < 4; i++ {
		if nested, ok := p["project"].(map[string]any); ok {
			p = nested
		} else {
			break
		}
	}
	return p
}
func (s *Server) importArchive(w http.ResponseWriter, r *http.Request, uid string) (map[string]any, error) {
	var state map[string]any
	var files map[string][]byte
	name := "导入项目"
	var e error
	typ, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if typ == "application/zip" || typ == "application/x-zip-compressed" || typ == "multipart/form-data" {
		r.Body = http.MaxBytesReader(w, r.Body, archiveTotalLimit+1)
		var b []byte
		if typ == "multipart/form-data" {
			if e = r.ParseMultipartForm(8 << 20); e != nil {
				return nil, store.ErrInvalid
			}
			defer r.MultipartForm.RemoveAll()
			name = first(r.FormValue("name"), name)
			var found bool
			for _, parts := range r.MultipartForm.File {
				for _, part := range parts {
					if found {
						return nil, store.ErrInvalid
					}
					found = true
					f, err := part.Open()
					if err != nil {
						return nil, err
					}
					b, e = io.ReadAll(io.LimitReader(f, archiveTotalLimit+1))
					f.Close()
					if e != nil {
						return nil, e
					}
				}
			}
			if !found {
				return nil, store.ErrInvalid
			}
		} else {
			b, e = io.ReadAll(r.Body)
			if e != nil {
				return nil, e
			}
		}
		if int64(len(b)) > archiveTotalLimit {
			return nil, store.ErrInvalid
		}
		state, files, e = readArchiveZIP(b)
		if e != nil {
			return nil, e
		}
	} else {
		p, err := s.body(w, r)
		if err != nil {
			return nil, err
		}
		name = first(str(p, "name"), name)
		state = unwrapArchiveProject(p)
	}
	if state == nil || len(state) == 0 {
		return nil, store.ErrInvalid
	}
	np, e := s.Store.CreateProject(r.Context(), uid, name)
	if e != nil {
		return nil, e
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.Store.DeleteProject(context.WithoutCancel(r.Context()), uid, np.ID)
		}
	}()
	mapping := map[string]string{}
	for _, old := range archiveImagePaths(state) {
		if _, ok := mapping[old]; ok {
			continue
		}
		if files == nil {
			continue
		}
		data, ok := files[old]
		if !ok {
			continue
		}
		ext, err := imageExtension(data)
		if err != nil {
			return nil, err
		}
		saved, err := s.Store.WriteImage(r.Context(), uid, np.ID, "output/"+store.NewID("import")+ext, data)
		if err != nil {
			return nil, err
		}
		mapping[old] = saved
	}
	stale := 0
	rewriteArchiveImages(state, mapping, &stale)
	if e = s.saveState(r.Context(), uid, np.ID, state); e != nil {
		return nil, e
	}
	if e = s.Store.SetActiveProject(r.Context(), uid, np.ID); e != nil {
		return nil, e
	}
	committed = true
	list, e := s.compatProjects(r.Context(), uid)
	if e != nil {
		return nil, e
	}
	return map[string]any{"success": true, "project": np, "list": map[string]any{"items": list, "activeId": np.ID}, "pages": len(array(object(state["storyboard"])["pages"])), "staleImages": stale}, nil
}
