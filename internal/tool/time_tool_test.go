package tool

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

func execTime(t *testing.T, args map[string]any) map[string]any {
	t.Helper()
	res, err := NewTimeTool().Execute(context.Background(), args)
	if err != nil {
		t.Fatalf("%v: unexpected error: %v", args["operation"], err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("%v: unexpected result type %T", args["operation"], res)
	}
	return m
}

// 同一串时间在 format / diff / add 三个入口必须都能解析（以前只有 format 接受 RFC3339）。
func TestTimeAcceptsRFC3339Everywhere(t *testing.T) {
	const rfc = "2026-11-24T19:45:41+08:00"
	const plain = "2026-11-24 20:45:41"

	f := execTime(t, map[string]any{"operation": "format", "time_str": rfc, "format": "2006-01-02 15:04:05"})
	if f["formatted"] != "2026-11-24 19:45:41" {
		t.Fatalf("format: got %v", f["formatted"])
	}

	d := execTime(t, map[string]any{
		"operation": "diff",
		"time_str":  rfc,
		"time_str2": plain,
		"timezone":  "Asia/Shanghai",
	})
	if d["seconds"] != 3600 {
		t.Fatalf("diff seconds: got %v, want 3600", d["seconds"])
	}

	a := execTime(t, map[string]any{
		"operation": "add",
		"time_str":  rfc,
		"duration":  "1h",
		"timezone":  "Asia/Shanghai",
	})
	if a["formatted"] != "2026-11-24 20:45:41" {
		t.Fatalf("add: got %v", a["formatted"])
	}
}

// diff 的第二个时间历史上借 format 字段传递，必须继续可用并给出提示。
func TestTimeDiffLegacyFormatField(t *testing.T) {
	res := execTime(t, map[string]any{
		"operation": "diff",
		"time_str":  "2026-11-24 10:00:00",
		"format":    "2026-11-24 12:30:00",
	})
	if res["seconds"] != 9000 {
		t.Fatalf("seconds: got %v, want 9000", res["seconds"])
	}
	if note, _ := res["note"].(string); !strings.Contains(note, "time_str2") {
		t.Fatalf("expected legacy-field note, got %v", res["note"])
	}
}

func TestTimeDiffRequiresSecondTime(t *testing.T) {
	_, err := NewTimeTool().Execute(context.Background(), map[string]any{
		"operation": "diff",
		"time_str":  "2026-11-24 10:00:00",
	})
	if err == nil || !strings.Contains(err.Error(), "time_str2") {
		t.Fatalf("want time_str2 error, got %v", err)
	}
}

func TestTimeAddDayAndWeekUnits(t *testing.T) {
	cases := []struct {
		duration string
		want     string
	}{
		{"2d", "2026-11-26 10:00:00"},
		{"1d1h", "2026-11-25 11:00:00"},
		{"2d3h", "2026-11-26 13:00:00"},
		{"1w", "2026-12-01 10:00:00"},
		{"-1d", "2026-11-23 10:00:00"},
		{"90m", "2026-11-24 11:30:00"},
	}
	for _, tc := range cases {
		res := execTime(t, map[string]any{
			"operation": "add",
			"time_str":  "2026-11-24 10:00:00",
			"duration":  tc.duration,
			"timezone":  "UTC",
		})
		if res["formatted"] != tc.want {
			t.Errorf("add %s: got %v, want %v", tc.duration, res["formatted"], tc.want)
		}
	}
}

func TestTimeInvalidDurationMessageNamesUnits(t *testing.T) {
	_, err := NewTimeTool().Execute(context.Background(), map[string]any{
		"operation": "add",
		"duration":  "2 days",
	})
	if err == nil {
		t.Fatal("want error for '2 days'")
	}
	for _, want := range []string{"d (day)", "w (week)"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q should mention %q", err.Error(), want)
		}
	}
}

// 无时区输入按 timezone 解析，并且回显解析所用时区（UTC+8 用户最常见的误解点）。
func TestTimeParseAppliesTimezone(t *testing.T) {
	res := execTime(t, map[string]any{
		"operation": "parse",
		"time_str":  "2026-11-24 19:45:41",
		"timezone":  "Asia/Shanghai",
	})
	if res["timezone"] != "Asia/Shanghai" {
		t.Fatalf("timezone echo: got %v", res["timezone"])
	}
	wantUnix := time.Date(2026, 11, 24, 19, 45, 41, 0, time.FixedZone("CST", 8*3600)).Unix()
	if res["unix"] != wantUnix {
		t.Fatalf("unix: got %v, want %v", res["unix"], wantUnix)
	}
	if res["utc"] != "2026-11-24T11:45:41Z" {
		t.Fatalf("utc echo: got %v", res["utc"])
	}
}

func TestTimeParseUnixTimestamp(t *testing.T) {
	want := time.Date(2026, 11, 24, 11, 45, 41, 0, time.UTC)
	res := execTime(t, map[string]any{
		"operation": "parse",
		"time_str":  strconv.FormatInt(want.Unix(), 10),
		"timezone":  "UTC",
	})
	if res["iso"] != "2026-11-24T11:45:41Z" {
		t.Fatalf("iso: got %v", res["iso"])
	}
}

func TestTimeUnknownTimezoneIsAnError(t *testing.T) {
	_, err := NewTimeTool().Execute(context.Background(), map[string]any{
		"operation": "now",
		"timezone":  "Mars/Olympus",
	})
	if err == nil || !strings.Contains(err.Error(), "unknown timezone") {
		t.Fatalf("want unknown timezone error, got %v", err)
	}
}
