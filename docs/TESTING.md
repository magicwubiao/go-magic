# 测试指南

## 运行测试

### 运行所有测试

```bash
go test ./...
```

### 运行测试并显示详细输出

```bash
go test -v ./...
```

### 运行特定包的测试

```bash
go test ./internal/cortex/...
go test ./internal/perception/...
go test ./internal/cognition/...
go test ./internal/execution/...
go test ./internal/memory/...
```

---

## 基准测试

### 运行基准测试

```bash
go test -bench=. ./internal/cortex/...
```

### 指定基准时间

```bash
go test -bench=. -benchtime=5s ./internal/cortex/...
```

### 内存基准

```bash
go test -bench=. -benchmem ./internal/cortex/...
```

### 基准测试示例输出

```
goos: linux
goarch: amd64
pkg: github.com/magicwubiao/go-magic/internal/cortex
cpu: Intel(R) Xeon(R) CPU E5-2680 v4 @ 2.40GHz
BenchmarkPerception-8       100000         12.3 ns/op
BenchmarkCognition-8         50000         24.5 ns/op
BenchmarkExecution-8         30000         45.2 ns/op
```

---

## 代码覆盖率

### 生成覆盖率报告

```bash
go test -cover ./...
```

### 详细覆盖率报告

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

### 查看覆盖率百分比

```bash
go test -covermode=count ./... | grep -v "^ok"
```

---

## 集成测试

### 运行集成测试

集成测试需要配置环境变量：

```bash
export TEST_API_KEY=your-test-api-key
export TEST_PROVIDER=openai

go test -tags=integration ./...
```

### 本地集成测试

部分集成测试可以使用 mock：

```bash
go test -short ./...
```

---

## 测试组织

### 测试文件命名

```
package_test.go  - 包级测试
package_int_test.go - 集成测试
package_bench_test.go - 基准测试
```

### 示例测试结构

```go
package cortex_test

import (
    "testing"
    "github.com/magicwubiao/go-magic/internal/cortex"
)

func TestNewManager(t *testing.T) {
    mgr := cortex.NewManager("/tmp/test")
    if mgr == nil {
        t.Fatal("Expected non-nil manager")
    }
}

func TestIntegration(t *testing.T) {
    // Skip if short mode
    if testing.Short() {
        t.Skip("Skipping integration test in short mode")
    }
    // Integration test code
}
```

---

## 测试工具

### Mock 提供者

项目提供了测试用的 mock LLM 提供者：

```go
import "github.com/magicwubiao/go-magic/internal/provider/mock"

provider := mock.NewProvider(mock.Config{
    Responses: []mock.Response{
        {Input: "hello", Output: "Hi there!"},
    },
})
```

### 测试工具

```go
import "github.com/magicwubiao/go-magic/internal/testutil"

// 临时目录
tempDir := testutil.TempDir(t)
defer tempDir.Cleanup()

// 临时文件
tempFile := testutil.TempFile(t, "test", []byte("content"))
defer tempFile.Close()
```

---

## CI/CD

### GitHub Actions

项目使用 GitHub Actions 进行持续集成：

```yaml
# .github/workflows/test.yml
name: Test
on: [push, pull_request]
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.21'
      - run: go test -v -race ./...
```

---

## 持续集成检查

本地运行 CI 检查：

```bash
# 格式化检查
make fmt

# 代码检查
make lint

# Vet 检查
make vet

# 运行所有检查
make ci
```

---

## 测试覆盖率目标

| 模块 | 目标覆盖率 |
|------|-----------|
| cortex | 80% |
| perception | 85% |
| cognition | 80% |
| execution | 75% |
| memory | 80% |
| skills | 70% |
