package server

import "testing"

// moveItem 用的是"摘除 + 在后备空间里搬移"的经典切片技巧，边界（拖到头部、
// 拖到末尾、原地不动、越界夹取）很容易写错且肉眼难查，这里逐条钉住。
//
// 最关键的一条：目标下标是在**摘除之前**的坐标系里给出的。摘除会让目标
// 之后的所有元素左移一格，如果按摘除后的下标插入，就会稳定地少挪一位。
func TestSessionQueueMoveItem(t *testing.T) {
	build := func() (*sessionQueue, []string) {
		q := newSessionQueue()
		ids := []string{"a", "b", "c", "d", "e"}
		for _, id := range ids {
			q.enqueue(&queuedTurn{id: id, content: id})
		}
		return q, ids
	}

	order := func(q *sessionQueue) string {
		q.mu.Lock()
		defer q.mu.Unlock()
		out := ""
		for _, it := range q.items {
			out += it.id
		}
		return out
	}

	cases := []struct {
		name      string
		id        string
		to        int
		want      string
		wantOK    bool
		wantMoved bool
	}{
		{"向后移动一位", "a", 1, "bacde", true, true},
		{"向后跨越到中间", "a", 2, "bcade", true, true},
		{"向后移动到末尾", "a", 4, "bcdea", true, true},
		{"向前移动一位", "c", 1, "acbde", true, true},
		{"向前移动到头部", "d", 0, "dabce", true, true},
		{"中间元素向后", "b", 3, "acdbe", true, true},
		{"原地不动", "c", 2, "abcde", true, false},
		{"负数夹到头部", "e", -1, "eabcd", true, true},
		{"越界夹到末尾", "a", 99, "bcdea", true, true},
		{"未知 id", "zzz", 0, "abcde", false, false},
		{"空 id", "", 0, "abcde", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, _ := build()
			ok, moved := q.moveItem(tc.id, tc.to)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if moved != tc.wantMoved {
				t.Fatalf("moved = %v, want %v", moved, tc.wantMoved)
			}
			if got := order(q); got != tc.want {
				t.Fatalf("order = %q, want %q", got, tc.want)
			}
		})
	}
}

// moveItem 不能碰正在执行的回合，也不能凭空多出/少掉队列项。
func TestSessionQueueMoveItemKeepsLength(t *testing.T) {
	q := newSessionQueue()
	for _, id := range []string{"a", "b", "c"} {
		q.enqueue(&queuedTurn{id: id})
	}
	// 模拟"正在执行"：worker 已把队头摘走，activeID 指向它。
	q.mu.Lock()
	q.running = true
	q.activeID = "a"
	q.items = q.items[1:]
	q.mu.Unlock()

	if ok, moved := q.moveItem("a", 0); ok || moved {
		t.Fatalf("正在执行的回合不应能被拖动: ok=%v moved=%v", ok, moved)
	}

	if ok, moved := q.moveItem("c", 0); !ok || !moved {
		t.Fatalf("排队项应可拖动: ok=%v moved=%v", ok, moved)
	}
	q.mu.Lock()
	n := len(q.items)
	active := q.activeID
	q.mu.Unlock()
	if n != 2 {
		t.Fatalf("队列长度被改变: %d, want 2", n)
	}
	if active != "a" {
		t.Fatalf("activeID 被改动: %q, want %q", active, "a")
	}
}
