package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Secret 密钥条目
type Secret struct {
	Key      string            `json:"key"`      // 密钥名称
	Provider string            `json:"provider"`  // 提供商 (openai, anthropic, etc.)
	Value    string            `json:"value"`     // 加密后的值
	Metadata map[string]string `json:"metadata"`  // 额外元数据
}

// Store 密钥存储
type Store struct {
	mu       sync.RWMutex
	dataDir  string
	secrets  map[string]*Secret
	key      []byte // 加密密钥
	masterKey []byte // 主密钥 (从密码派生)
}

// NewStore 创建新的密钥存储
func NewStore(dataDir string) (*Store, error) {
	s := &Store{
		dataDir: dataDir,
		secrets: make(map[string]*Secret),
	}

	// 创建目录
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, err
	}

	// 加载现有密钥
	if err := s.load(); err != nil {
		// 首次使用，生成新密钥
		s.masterKey = s.generateMasterKey()
	}

	return s, nil
}

// generateMasterKey 生成主密钥
func (s *Store) generateMasterKey() []byte {
	// 从随机数据生成 32 字节密钥
	key := make([]byte, 32)
	rand.Read(key)
	return key
}

// deriveKey 从主密钥派生加密密钥
func (s *Store) deriveKey() ([]byte, error) {
	if s.masterKey == nil {
		// 尝试加载
		if err := s.loadMasterKey(); err != nil {
			return nil, err
		}
	}

	// 使用 HKDF 派生 32 字节 AES 密钥
	hash := sha256.Sum256(s.masterKey)
	return hash[:], nil
}

// Set 设置密钥
func (s *Store) Set(key, provider, value string, metadata map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 加密值
	encrypted, err := s.encrypt(value)
	if err != nil {
		return fmt.Errorf("failed to encrypt: %w", err)
	}

	s.secrets[key] = &Secret{
		Key:      key,
		Provider: provider,
		Value:    encrypted,
		Metadata: metadata,
	}

	return s.save()
}

// Get 获取密钥（解密）
func (s *Store) Get(key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	secret, ok := s.secrets[key]
	if !ok {
		return "", fmt.Errorf("secret '%s' not found", key)
	}

	// 解密
	return s.decrypt(secret.Value)
}

// GetByProvider 根据提供商获取所有密钥
func (s *Store) GetByProvider(provider string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]string)
	for key, secret := range s.secrets {
		if secret.Provider == provider {
			decrypted, err := s.decrypt(secret.Value)
			if err != nil {
				continue
			}
			result[key] = decrypted
		}
	}

	return result, nil
}

// Delete 删除密钥
func (s *Store) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.secrets[key]; !ok {
		return fmt.Errorf("secret '%s' not found", key)
	}

	delete(s.secrets, key)
	return s.save()
}

// List 列出所有密钥名称
func (s *Store) List() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, 0, len(s.secrets))
	for k := range s.secrets {
		keys = append(keys, k)
	}
	return keys
}

// ListByProvider 列出提供商下的密钥
func (s *Store) ListByProvider(provider string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, 0)
	for k, secret := range s.secrets {
		if secret.Provider == provider {
			keys = append(keys, k)
		}
	}
	return keys
}

// Has 检查密钥是否存在
func (s *Store) Has(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.secrets[key]
	return ok
}

// GetMetadata 获取密钥元数据
func (s *Store) GetMetadata(key string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	secret, ok := s.secrets[key]
	if !ok {
		return nil, fmt.Errorf("secret '%s' not found", key)
	}

	return secret.Metadata, nil
}

// UpdateMetadata 更新密钥元数据
func (s *Store) UpdateMetadata(key string, metadata map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret, ok := s.secrets[key]
	if !ok {
		return fmt.Errorf("secret '%s' not found", key)
	}

	secret.Metadata = metadata
	return s.save()
}

// Rename 重命名密钥
func (s *Store) Rename(oldKey, newKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	secret, ok := s.secrets[oldKey]
	if !ok {
		return fmt.Errorf("secret '%s' not found", oldKey)
	}

	secret.Key = newKey
	s.secrets[newKey] = secret
	delete(s.secrets, oldKey)

	return s.save()
}

// encrypt 加密数据
func (s *Store) encrypt(plaintext string) (string, error) {
	key, err := s.deriveKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// decrypt 解密数据
func (s *Store) decrypt(ciphertext string) (string, error) {
	key, err := s.deriveKey()
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// save 保存到文件
func (s *Store) save() error {
	secretsPath := filepath.Join(s.dataDir, "secrets.enc")
	
	data, err := json.MarshalIndent(s.secrets, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(secretsPath, data, 0600)
}

// load 加载文件
func (s *Store) load() error {
	secretsPath := filepath.Join(s.dataDir, "secrets.enc")

	data, err := os.ReadFile(secretsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	return json.Unmarshal(data, &s.secrets)
}

// saveMasterKey 保存主密钥
func (s *Store) saveMasterKey() error {
	keyPath := filepath.Join(s.dataDir, ".masterkey")

	// 用系统密钥环或环境变量加密存储
	// 简化版本：直接 base64 存储
	encoded := base64.StdEncoding.EncodeToString(s.masterKey)
	return os.WriteFile(keyPath, []byte(encoded), 0600)
}

// loadMasterKey 加载主密钥
func (s *Store) loadMasterKey() error {
	keyPath := filepath.Join(s.dataDir, ".masterkey")

	data, err := os.ReadFile(keyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	s.masterKey, err = base64.StdEncoding.DecodeString(string(data))
	return err
}

// ExportKeys 导出密钥列表（不含值）
func (s *Store) ExportKeys() []Secret {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]Secret, 0, len(s.secrets))
	for _, secret := range s.secrets {
		result = append(result, Secret{
			Key:      secret.Key,
			Provider: secret.Provider,
			Metadata: secret.Metadata,
		})
	}
	return result
}

// ImportFromEnv 从环境变量导入
func (s *Store) ImportFromEnv() error {
	providers := []string{"openai", "anthropic", "deepseek", "google", "azure", "openrouter", "ollama"}

	for _, provider := range providers {
		// 常见环境变量名
		envVars := []string{
			fmt.Sprintf("%s_API_KEY", provider),
			fmt.Sprintf("%s_API_KEY_%s", provider, "KEY"),
			fmt.Sprintf("%s_TOKEN", provider),
		}

		for _, envVar := range envVars {
			if value := os.Getenv(envVar); value != "" {
				key := fmt.Sprintf("%s_api_key", provider)
				if err := s.Set(key, provider, value, map[string]string{
					"source":    "environment",
					"env_var":   envVar,
				}); err == nil {
					fmt.Printf("Imported %s from %s\n", key, envVar)
				}
			}
		}
	}

	return nil
}
