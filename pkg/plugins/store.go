package plugins

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// storeVersion 变更时旧记录直接丢弃：安装记录只用于"列出/卸载"，
// 丢了最坏情况是用户重新装一次，比带着不兼容结构跑下去安全。
const storeVersion = 1

// Record 是一条安装记录。
//
// 内含完整 Manifest 而不是只存名字：卸载时要判断"配置里这一项还是不是
// 插件当初写进去的那份"，只存名字就只能盲删，会把用户改过的配置一起带走。
type Record struct {
	Manifest
	InstalledAt time.Time `json:"installed_at"`
	// Source 是安装来源（plugin.json 的路径），仅用于展示与排查。
	Source string `json:"source,omitempty"`
}

type storeFile struct {
	Version int      `json:"version"`
	Plugins []Record `json:"plugins"`
}

// Store 管理安装记录，落盘 <magicHome>/plugins/installed.json。
type Store struct {
	path string
	mu   sync.Mutex
}

// NewStore 构造安装记录存储。path 为空时 Store 只做内存操作（测试用）。
func NewStore(path string) *Store { return &Store{path: path} }

// Path 返回记录文件路径。
func (s *Store) Path() string { return s.path }

// List 返回按名字排序的安装记录。
func (s *Store) List() ([]Record, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(f.Plugins, func(i, j int) bool { return f.Plugins[i].Name < f.Plugins[j].Name })
	return f.Plugins, nil
}

// Get 按名字取一条记录。
func (s *Store) Get(name string) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return Record{}, false, err
	}
	for _, rec := range f.Plugins {
		if rec.Name == name {
			return rec, true, nil
		}
	}
	return Record{}, false, nil
}

// Put 写入（同名替换）一条记录。
func (s *Store) Put(rec Record) error {
	if err := ValidateName(rec.Name); err != nil {
		return err
	}
	if rec.InstalledAt.IsZero() {
		rec.InstalledAt = time.Now()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return err
	}
	replaced := false
	for i := range f.Plugins {
		if f.Plugins[i].Name == rec.Name {
			f.Plugins[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		f.Plugins = append(f.Plugins, rec)
	}
	return s.saveLocked(f)
}

// Delete 删除一条记录并返回被删的那条。
func (s *Store) Delete(name string) (Record, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.loadLocked()
	if err != nil {
		return Record{}, false, err
	}
	for i := range f.Plugins {
		if f.Plugins[i].Name == name {
			rec := f.Plugins[i]
			f.Plugins = append(f.Plugins[:i], f.Plugins[i+1:]...)
			if err := s.saveLocked(f); err != nil {
				return Record{}, false, err
			}
			return rec, true, nil
		}
	}
	return Record{}, false, nil
}

// loadLocked 读盘；文件不存在/损坏都退化成空记录集（记录不是事实来源，
// 配置才是；读不到只影响"能列出哪些插件"）。
func (s *Store) loadLocked() (storeFile, error) {
	if s.path == "" {
		return storeFile{Version: storeVersion}, nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return storeFile{Version: storeVersion}, nil
		}
		return storeFile{}, fmt.Errorf("read installed plugins: %w", err)
	}
	if len(data) == 0 {
		return storeFile{Version: storeVersion}, nil
	}
	var f storeFile
	if err := json.Unmarshal(data, &f); err != nil {
		return storeFile{Version: storeVersion}, nil
	}
	if f.Version != storeVersion {
		return storeFile{Version: storeVersion}, nil
	}
	return f, nil
}

// saveLocked 原子写（temp + rename）：半截 JSON 会让所有安装记录一起丢。
func (s *Store) saveLocked(f storeFile) error {
	if s.path == "" {
		return nil
	}
	f.Version = storeVersion
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create plugin dir: %w", err)
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".installed-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_ = os.Chmod(tmpName, 0o600)
	return os.Rename(tmpName, s.path)
}
