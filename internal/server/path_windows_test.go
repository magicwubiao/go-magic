//go:build windows

package server

import "testing"

// mergePathList 的去重与排序约定：注册表系统 PATH 在前、用户 PATH 次之、
// 进程独有条目垫底；同一条目（大小写不敏感）只出现一次。
func TestMergePathList(t *testing.T) {
	got := mergePathList(
		[]string{`C:\Apps\Extra`, `C:\Windows`, `C:\Windows\system32`},
		[]string{`C:\Windows`, `C:\Windows\System32`, `C:\Program Files\nodejs`},
		[]string{`C:\Users\me\AppData\Local\Programs\Python`, `%SystemRoot%`},
	)
	// 同一条目只保留先出现者（machine PATH 优先级最高）；
	// 进程独有的 C:\Apps\Extra 垫底。
	want := `C:\Windows;C:\Windows\System32;C:\Program Files\nodejs;C:\Users\me\AppData\Local\Programs\Python;%SystemRoot%;C:\Apps\Extra`
	if got != want {
		t.Fatalf("mergePathList mismatch:\n got: %s\nwant: %s", got, want)
	}
}

// 空条目与首尾空白应被剔除，不能混进 PATH。
func TestMergePathListSkipsBlankEntries(t *testing.T) {
	got := mergePathList(
		[]string{"  ", `C:\Windows`, ""},
		[]string{` C:\Windows `},
		nil,
	)
	want := `C:\Windows`
	if got != want {
		t.Fatalf("mergePathList = %q, want %q", got, want)
	}
}
