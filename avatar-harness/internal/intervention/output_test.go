package intervention

import "testing"

func TestCurrentResponseConstraints(t *testing.T) {
	for _, tc := range []struct {
		user string
		want ResponseConstraints
	}{
		{"现在用两句话概括，不要再问我。", ResponseConstraints{Sentences: 2, NoQuestions: true}},
		{"先不要再问。", ResponseConstraints{NoQuestions: true}},
		{"给我三句话的复盘框架。", ResponseConstraints{Sentences: 3}},
		{"请用2句话回答。", ResponseConstraints{Sentences: 2}},
		{"我愿意试一个不超过一分钟的小行动。", ResponseConstraints{ActionMinutes: 1}},
		{"不要用两句话敷衍我。", ResponseConstraints{}},
		{"报告引用：‘不用提问’，他写道“用两句话回答，不要再问我”。", ResponseConstraints{}},
		{"“请用三句话回答。”现在用两句话回答。", ResponseConstraints{Sentences: 2}},
		{"我昨天说过用两句话，但今天请详细解释。", ResponseConstraints{}},
		{"最多两分钟的是会议，不是行动。", ResponseConstraints{}},
	} {
		if got := CurrentResponseConstraints(tc.user); got != tc.want {
			t.Errorf("%q: got %+v want %+v", tc.user, got, tc.want)
		}
	}
}

func TestResponseConstraintCounts(t *testing.T) {
	for _, text := range []string{"第一点；第二点。", "有3.5小时。还有一点。", "他说“可以。”接下来再看。"} {
		want := 2
		if text == "第一点；第二点。" {
			want = 1
		}
		if got := SentenceCount(text); got != want {
			t.Errorf("%q: %d", text, got)
		}
	}
	if len((ResponseConstraints{Sentences: 2, NoQuestions: true}).Check("你希望先被征询；你也希望自己决定。")) != 1 {
		t.Fatal("semicolon must not satisfy explicit two sentences")
	}
}
