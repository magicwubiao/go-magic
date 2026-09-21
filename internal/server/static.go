package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Skip API routes
	if strings.HasPrefix(path, "/api/") {
		http.Error(w, "not found", 404)
		return
	}

	// Dashboard plugins path
	if strings.HasPrefix(path, "/dashboard-plugins/") {
		subPath := strings.TrimPrefix(path, "/dashboard-plugins/")
		// 防止路径遍历
		if strings.Contains(subPath, "..") || strings.Contains(subPath, "\\") {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		pluginPath := filepath.Join(s.magicHome, "plugins", subPath)
		// 验证最终路径仍在 plugins 目录内
		absPluginPath, _ := filepath.Abs(pluginPath)
		absPluginsDir, _ := filepath.Abs(filepath.Join(s.magicHome, "plugins"))
		if !strings.HasPrefix(absPluginPath, absPluginsDir+string(filepath.Separator)) && absPluginPath != absPluginsDir {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}
		if data, err := os.ReadFile(pluginPath); err == nil {
			contentType := getContentType(pluginPath)
			w.Header().Set("Content-Type", contentType)
			w.Write(data)
			return
		}
		http.Error(w, "not found", 404)
		return
	}

	// Serve embedded SPA files
	serveSPA(w, r)
}

func getContentType(path string) string {
	ext := strings.ToLower(path[strings.LastIndex(path, ".")+1:])
	switch ext {
	case "js":
		return "application/javascript"
	case "css":
		return "text/css"
	case "html":
		return "text/html"
	case "json":
		return "application/json"
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "svg":
		return "image/svg+xml"
	case "woff", "woff2":
		return "font/woff2"
	case "ttf":
		return "font/ttf"
	case "eot":
		return "application/vnd.ms-fontobject"
	case "ico":
		return "image/x-icon"
	default:
		return "application/octet-stream"
	}
}

func serveSPA(w http.ResponseWriter, r *http.Request) {
	// 票据虽然已经从「登录凭据」降级为「单文件、短时效」，它终究还是出现在
	// URL 里。Referrer-Policy 必须落在**文档响应**上才能真正生效：它约束的是
	// 本页发起的后续请求要不要带 Referer，而不是这条 API 响应本身。
	// no-referrer 确保页面里任何外链都拿不到我们 URL 上的票据。
	w.Header().Set("Referrer-Policy", "no-referrer")

	path := r.URL.Path

	// Remove leading slash
	if path == "/" {
		path = "/index.html"
	}
	path = strings.TrimPrefix(path, "/")

	// Try to serve the file from embedded dist
	fullPath := "dist/" + path
	data, err := distFS.ReadFile(fullPath)
	if err != nil {
		// If file not found, serve index.html for SPA routing
		data, err = distFS.ReadFile("dist/index.html")
		if err != nil {
			http.Error(w, "not found", 404)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
		return
	}

	contentType := getContentType(fullPath)
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}
