package server

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"avatarharness/internal/config"
)

func (a *App) creatorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/characters/import", a.importCharacter)
	if a.Config.AvatarCreator == nil {
		return
	}
	target, err := url.Parse(a.Config.AvatarCreator.BaseURL)
	if err != nil || target.Host == "" {
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	proxy.Transport = transport
	proxy.FlushInterval = -1
	director := proxy.Director
	proxy.Director = func(r *http.Request) {
		proto := r.Header.Get("X-Forwarded-Proto")
		if proto == "" {
			proto = "http"
			if r.TLS != nil {
				proto = "https"
			}
		}
		host := r.Host
		director(r)
		r.Header.Set("X-Forwarded-Host", host)
		r.Header.Set("X-Forwarded-Proto", proto)
		// The creator has no cloud credentials. Do not forward gateway login.
		r.Header.Del("Authorization")
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		problem(w, 502, "形象制作暂时无法连接，请稍后重试")
	}
	mux.HandleFunc("GET /create-avatar", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/create-avatar/", http.StatusTemporaryRedirect)
	})
	mux.Handle("/create-avatar/", http.StripPrefix("/create-avatar", proxy))
}

func (a *App) importCharacter(w http.ResponseWriter, r *http.Request) {
	if a.Config.AvatarCreator == nil {
		problem(w, 404, "形象制作未配置")
		return
	}
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/zip" {
		problem(w, 415, "请上传 LAM ZIP 资产")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if len([]rune(name)) > 60 {
		problem(w, 422, "角色名称最多 60 字")
		return
	}
	if name == "" {
		name = "我的形象 · " + time.Now().Format("01-02 15:04")
	}
	dir := a.Runtime.Store.CharacterAssetDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		problem(w, 500, "无法创建角色目录")
		return
	}
	f, err := os.CreateTemp(dir, ".import-*.zip")
	if err != nil {
		problem(w, 500, "无法保存角色资产")
		return
	}
	defer os.Remove(f.Name())
	defer f.Close()
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	hash := sha256.New()
	if _, err = io.Copy(io.MultiWriter(f, hash), r.Body); err != nil {
		problem(w, 400, "ZIP 读取失败或超过 64 MB")
		return
	}
	info, err := f.Stat()
	if err != nil || !validLAMArchive(f, info.Size()) {
		problem(w, 422, "不是完整的 LAM 资产：需要 skin.glb、offset.ply 和 animation.glb")
		return
	}
	id := fmt.Sprintf("lam-%x", hash.Sum(nil)[:12])
	if existing, ok := a.Runtime.Character(id); ok {
		write(w, 200, existing)
		return
	}
	asset := filepath.Join(dir, id+".zip")
	if err = f.Close(); err == nil {
		err = os.Rename(f.Name(), asset)
	}
	if err != nil {
		problem(w, 500, "角色资产保存失败")
		return
	}
	ch := a.Config.Characters[0]
	ch.ID, ch.Name, ch.AssetPath = id, name, asset
	ch.AvatarProvider = nil
	ch.Renderer = config.Renderer{Kind: "lam", Driver: "remote", SpeechJawMax: .32, AssetURL: "/api/characters/" + id + "/asset/avatar.zip"}
	ch, created, err := a.Runtime.Store.AddCharacter(ch)
	if err != nil {
		problem(w, 500, "资产已保留，但角色列表保存失败，请重新保存")
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	write(w, status, ch)
}

func validLAMArchive(f *os.File, size int64) bool {
	r, err := zip.NewReader(f, size)
	if err != nil || len(r.File) > 128 {
		return false
	}
	required := map[string]bool{"skin.glb": false, "offset.ply": false, "animation.glb": false}
	var unpacked uint64
	for _, item := range r.File {
		name := item.Name
		if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || path.Clean(name) != strings.TrimSuffix(name, "/") || name == ".." || strings.HasPrefix(name, "../") {
			return false
		}
		unpacked += item.UncompressedSize64
		if unpacked > 512<<20 {
			return false
		}
		if _, ok := required[path.Base(name)]; ok && item.UncompressedSize64 > 0 {
			required[path.Base(name)] = true
		}
	}
	return required["skin.glb"] && required["offset.ply"] && required["animation.glb"]
}
