package gateway

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ============================================================================
// AES Encryption/Decryption (ECB mode)
//
// WeChat iLink uses AES-128-ECB for CDN media encryption.
// Note: ECB mode is used by the WeChat protocol; in other contexts,
// use a more secure mode like GCM.
// ============================================================================

// pkcs7Pad pads data to the given block size.
func pkcs7Pad(src []byte, blockSize int) []byte {
	padding := blockSize - len(src)%blockSize
	if padding == 0 {
		padding = blockSize
	}
	out := make([]byte, len(src)+padding)
	copy(out, src)
	for i := len(src); i < len(out); i++ {
		out[i] = byte(padding)
	}
	return out
}

// pkcs7Unpad removes PKCS7 padding.
func pkcs7Unpad(src []byte, blockSize int) ([]byte, error) {
	if len(src) == 0 || len(src)%blockSize != 0 {
		return nil, fmt.Errorf("invalid padded data size %d", len(src))
	}
	padding := int(src[len(src)-1])
	if padding <= 0 || padding > blockSize || padding > len(src) {
		return nil, fmt.Errorf("invalid padding size %d", padding)
	}
	for i := len(src) - padding; i < len(src); i++ {
		if src[i] != byte(padding) {
			return nil, fmt.Errorf("invalid padding content")
		}
	}
	return src[:len(src)-padding], nil
}

// EncryptAESECB encrypts data using AES-128-ECB with PKCS7 padding.
func EncryptAESECB(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plaintext, block.BlockSize())
	out := make([]byte, len(padded))
	for i := 0; i < len(padded); i += block.BlockSize() {
		block.Encrypt(out[i:i+block.BlockSize()], padded[i:i+block.BlockSize()])
	}
	return out, nil
}

// DecryptAESECB decrypts data using AES-128-ECB with PKCS7 unpad.
func DecryptAESECB(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("invalid ciphertext size %d", len(ciphertext))
	}
	out := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += block.BlockSize() {
		block.Decrypt(out[i:i+block.BlockSize()], ciphertext[i:i+block.BlockSize()])
	}
	return pkcs7Unpad(out, block.BlockSize())
}

// ============================================================================
// CDN Media Download
// ============================================================================

// CDNDownloader handles downloading and decrypting media from WeChat CDN.
type CDNDownloader struct {
	client    *http.Client
	cdnBaseURL string
}

// NewCDNDownloader creates a CDN downloader.
func NewCDNDownloader(cdnBaseURL string, proxy string) *CDNDownloader {
	client := &http.Client{Timeout: 60 * time.Second}
	if proxy != "" {
		if proxyURL, err := url.Parse(proxy); err == nil {
			if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
				transport := defaultTransport.Clone()
				transport.Proxy = http.ProxyURL(proxyURL)
				client.Transport = transport
			}
		}
	}
	if cdnBaseURL == "" {
		cdnBaseURL = ilinkDefaultCDNBaseURL
	}
	return &CDNDownloader{
		client:     client,
		cdnBaseURL: strings.TrimRight(cdnBaseURL, "/"),
	}
}

// DownloadMedia downloads and decrypts media from the CDN.
// If key is nil or empty, the data is returned as-is (no decryption).
func (d *CDNDownloader) DownloadMedia(ctx context.Context, encryptedQueryParam, fullURL string, key []byte) ([]byte, error) {
	// Build candidate URLs
	var candidates []string
	if fullURL != "" {
		candidates = append(candidates, strings.TrimSpace(fullURL))
	}
	if encryptedQueryParam != "" {
		candidates = append(candidates, d.cdnBaseURL+
			"/download?encrypted_query_param="+url.QueryEscape(encryptedQueryParam))
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("missing CDN download URL")
	}

	var lastErr error
	for _, downloadURL := range candidates {
		for attempt := 1; attempt <= ilinkDownloadRetryMax; attempt++ {
			data, err := d.downloadOnce(ctx, downloadURL)
			if err == nil {
				if len(key) > 0 {
					return DecryptAESECB(data, key)
				}
				return data, nil
			}
			lastErr = err
			if attempt < ilinkDownloadRetryMax {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(ilinkDownloadRetryDelay):
				}
			}
		}
	}
	return nil, fmt.Errorf("cdn download failed: %w", lastErr)
}

func (d *CDNDownloader) downloadOnce(ctx context.Context, downloadURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("cdn HTTP %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, ilinkMediaMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > ilinkMediaMaxBytes {
		return nil, fmt.Errorf("media too large: %d bytes", len(data))
	}
	return data, nil
}

// ============================================================================
// CDN Media Upload
// ============================================================================

// CDNUploader handles uploading and encrypting media to WeChat CDN.
type CDNUploader struct {
	api       *ILinkAPIClient
	client    *http.Client
	cdnBaseURL string
}

// NewCDNUploader creates a CDN uploader.
func NewCDNUploader(api *ILinkAPIClient, cdnBaseURL string, proxy string) *CDNUploader {
	client := &http.Client{Timeout: 120 * time.Second}
	if proxy != "" {
		if proxyURL, err := url.Parse(proxy); err == nil {
			if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
				transport := defaultTransport.Clone()
				transport.Proxy = http.ProxyURL(proxyURL)
				client.Transport = transport
			}
		}
	}
	if cdnBaseURL == "" {
		cdnBaseURL = ilinkDefaultCDNBaseURL
	}
	return &CDNUploader{
		api:        api,
		client:     client,
		cdnBaseURL: strings.TrimRight(cdnBaseURL, "/"),
	}
}

// UploadFile uploads a file to the WeChat CDN and returns the download param.
func (u *CDNUploader) UploadFile(ctx context.Context, localPath, filename, toUserID string, mediaType int) (downloadParam, aesKeyHex string, err error) {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return "", "", err
	}
	if len(data) > ilinkMediaMaxBytes {
		return "", "", fmt.Errorf("media too large: %d bytes", len(data))
	}

	// Generate filekey and AES key
	filekey := randomHex(16)
	aesKey := make([]byte, 16)
	if _, err := rand.Read(aesKey); err != nil {
		return "", "", err
	}
	aesKeyHex = hex.EncodeToString(aesKey)
	rawMD5 := md5.Sum(data)

	// Get upload URL
	resp, err := u.api.GetUploadURL(ctx, ILinkGetUploadURLReq{
		Filekey:     filekey,
		MediaType:   mediaType,
		ToUserID:    toUserID,
		Rawsize:     int64(len(data)),
		RawfileMD5:  hex.EncodeToString(rawMD5[:]),
		Filesize:    aesEcbPaddedSize(int64(len(data))),
		NoNeedThumb: true,
		Aeskey:      aesKeyHex,
	})
	if err != nil {
		return "", "", err
	}
	if resp.Ret != 0 || resp.Errcode != 0 {
		return "", "", fmt.Errorf("getuploadurl: ret=%d errcode=%d",
			resp.Ret, resp.Errcode)
	}

	uploadParam := strings.TrimSpace(resp.UploadParam)
	uploadFullURL := strings.TrimSpace(resp.UploadFullURL)
	if uploadParam == "" && uploadFullURL == "" {
		return "", "", fmt.Errorf("no upload URL returned")
	}

	// Encrypt and upload
	ciphertext, err := EncryptAESECB(data, aesKey)
	if err != nil {
		return "", "", err
	}

	uploadURL := uploadFullURL
	if uploadURL == "" {
		uploadURL = u.cdnBaseURL + "/upload?encrypted_query_param=" +
			url.QueryEscape(uploadParam) + "&filekey=" + url.QueryEscape(filekey)
	}

	var lastErr error
	for attempt := 1; attempt <= ilinkUploadRetryMax; attempt++ {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost,
			uploadURL, bytes.NewReader(ciphertext))
		if reqErr != nil {
			return "", "", reqErr
		}
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, doErr := u.client.Do(req)
		if doErr != nil {
			lastErr = doErr
			continue
		}
		func() {
			defer resp.Body.Close()
			if resp.StatusCode >= 400 && resp.StatusCode < 500 {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
				lastErr = fmt.Errorf("upload client error %d: %s",
					resp.StatusCode, strings.TrimSpace(string(body)))
				return
			}
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
				lastErr = fmt.Errorf("upload server error %d: %s",
					resp.StatusCode, strings.TrimSpace(string(body)))
				return
			}
			if encrypted := strings.TrimSpace(resp.Header.Get("X-Encrypted-Param")); encrypted != "" {
				lastErr = nil
				uploadParam = encrypted
				return
			}
			lastErr = fmt.Errorf("missing X-Encrypted-Param header")
		}()
		if lastErr == nil {
			return uploadParam, aesKeyHex, nil
		}
		if strings.Contains(lastErr.Error(), "client error") ||
			attempt == ilinkUploadRetryMax {
			break
		}
	}

	return "", "", lastErr
}

// ============================================================================
// Media Type Detection
// ============================================================================

// DetectMediaMetadata determines file name and content type from data.
func DetectMediaMetadata(data []byte, fallbackName, fallbackContentType string) (string, string) {
	contentType := strings.TrimSpace(fallbackContentType)
	ext := filepath.Ext(fallbackName)

	// Try to detect from magic bytes
	contentTypeFromData := http.DetectContentType(data)
	if contentTypeFromData != "application/octet-stream" {
		contentType = contentTypeFromData
	}

	// Get extension from content type if needed
	if ext == "" && contentType != "" {
		if exts, err := mime.ExtensionsByType(contentType); err == nil && len(exts) > 0 {
			ext = exts[0]
		}
	}

	filename := sanitizeFilename(fallbackName)
	if filename == "" {
		filename = "media"
	}
	if filepath.Ext(filename) == "" && ext != "" {
		filename += ext
	}

	return filename, contentType
}

// sanitizeFilename cleans a filename to prevent path traversal.
func sanitizeFilename(name string) string {
	name = filepath.Base(strings.TrimSpace(name))
	if name == "." || name == "/" || name == "" {
		return ""
	}
	return name
}

// ============================================================================
// Helpers
// ============================================================================

// aesEcbPaddedSize calculates the size after AES-ECB encryption with PKCS7 padding.
func aesEcbPaddedSize(size int64) int64 {
	return (size/16 + 1) * 16
}

// EncodeWeixinAESKey encodes a hex AES key to base64 format used by the API.
func EncodeWeixinAESKey(aesKeyHex string) string {
	return base64.StdEncoding.EncodeToString([]byte(aesKeyHex))
}

// ParseWeixinMediaAESKey parses a WeChat media AES key (base64 to raw bytes).
func ParseWeixinMediaAESKey(aesKeyBase64 string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(aesKeyBase64)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 16 {
		return decoded, nil
	}
	if len(decoded) == 32 {
		if raw, err := hex.DecodeString(string(decoded)); err == nil && len(raw) == 16 {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("unsupported aes_key length %d", len(decoded))
}

// downloadFilenameFromURL extracts a filename from a URL.
func downloadFilenameFromURL(rawURL, fallback string) string {
	if name := sanitizeFilename(fallback); name != "" {
		return name
	}
	parsed, err := url.Parse(rawURL)
	if err == nil {
		if base := sanitizeFilename(path.Base(parsed.Path)); base != "" {
			return base
		}
	}
	return "remote-media"
}
