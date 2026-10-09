package userfeedback_test

import (
	"errors"
	"testing"
	"time"

	"github.com/H4aruki/MyTechPulse/server/internal/userfeedback"
)

func ptr[T any](v T) *T { return &v }

func validForm() userfeedback.Form {
	return userfeedback.Form{ID: 1, Key: "service_satisfaction", Version: 1, Title: "t", CooldownDays: 60, Questions: []userfeedback.Question{
		{ID: 10, Key: "overall", Text: "q", SortOrder: 1, Required: true},
		{ID: 11, Key: "interest_match", Text: "q", SortOrder: 2, Required: true, DisplayIfQuestionID: ptr[int64](10), DisplayIfScoreMax: ptr[int32](2)},
		{ID: 12, Key: "freshness", Text: "q", SortOrder: 3, Required: true, DisplayIfQuestionID: ptr[int64](10), DisplayIfScoreMax: ptr[int32](2)},
		{ID: 13, Key: "usability", Text: "q", SortOrder: 4, Required: true, DisplayIfQuestionID: ptr[int64](10), DisplayIfScoreMax: ptr[int32](2)},
	}}
}

func TestValidateDefinitionAcceptsInitialForm(t *testing.T) {
	if err := userfeedback.ValidateDefinition(validForm()); err != nil {
		t.Fatal(err)
	}
}

func TestValidateDefinitionRejectsBrokenForms(t *testing.T) {
	cases := map[string]func(*userfeedback.Form){
		"総合評価が0問": func(f *userfeedback.Form) { f.Questions = f.Questions[1:] },
		"総合評価が2問": func(f *userfeedback.Form) {
			f.Questions[1].DisplayIfQuestionID, f.Questions[1].DisplayIfScoreMax = nil, nil
		},
		"追加質問が追加質問を参照": func(f *userfeedback.Form) { f.Questions[2].DisplayIfQuestionID = ptr[int64](11) },
		"しきい値が2以外":     func(f *userfeedback.Form) { f.Questions[1].DisplayIfScoreMax = ptr[int32](3) },
		"しきい値だけある":     func(f *userfeedback.Form) { f.Questions[0].DisplayIfScoreMax = ptr[int32](2) },
		"質問キーの重複":      func(f *userfeedback.Form) { f.Questions[2].Key = "interest_match" },
		"並び順の重複":       func(f *userfeedback.Form) { f.Questions[2].SortOrder = 2 },
		"再表示間隔が0日":     func(f *userfeedback.Form) { f.CooldownDays = 0 },
		"自分自身を参照":      func(f *userfeedback.Form) { f.Questions[3].DisplayIfQuestionID = ptr[int64](13) },
	}
	for name, mutate := range cases {
		f := validForm()
		mutate(&f)
		if err := userfeedback.ValidateDefinition(f); !errors.Is(err, userfeedback.ErrInvalidDefinition) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestValidateDefinitionAcceptsFormWithoutFollowups(t *testing.T) {
	f := validForm()
	f.Questions = f.Questions[:1]
	if err := userfeedback.ValidateDefinition(f); err != nil {
		t.Fatal(err)
	}
	needs, err := userfeedback.ValidateOverall(f, 10, 1)
	if err != nil || needs {
		t.Fatalf("追加質問が無い版では低評価でも完了する: %v %v", needs, err)
	}
}

func TestValidateOverall(t *testing.T) {
	f := validForm()
	for score, want := range map[int32]bool{1: true, 2: true, 3: false, 5: false} {
		got, err := userfeedback.ValidateOverall(f, 10, score)
		if err != nil || got != want {
			t.Errorf("score %d: %v %v", score, got, err)
		}
	}
	for _, bad := range []struct {
		id    int64
		score int32
	}{{10, 0}, {10, 6}, {11, 3}, {99, 3}} {
		if _, err := userfeedback.ValidateOverall(f, bad.id, bad.score); !errors.Is(err, userfeedback.ErrInvalidInput) {
			t.Errorf("%+v: err = %v", bad, err)
		}
	}
}

func TestValidateFollowup(t *testing.T) {
	f := validForm()
	ok := []userfeedback.Answer{{11, 1}, {12, 5}, {13, 3}}
	if err := userfeedback.ValidateFollowup(f, 2, ok); err != nil {
		t.Fatal(err)
	}
	bad := map[string][]userfeedback.Answer{
		"必須の未回答":   {{11, 1}, {12, 5}},
		"範囲外の値":    {{11, 1}, {12, 5}, {13, 6}},
		"総合評価を混ぜる": {{10, 1}, {11, 1}, {12, 5}, {13, 3}},
		"同じ質問を2回":  {{11, 1}, {11, 2}, {12, 5}, {13, 3}},
		"存在しない質問":  {{11, 1}, {12, 5}, {13, 3}, {99, 1}},
	}
	for name, answers := range bad {
		if err := userfeedback.ValidateFollowup(f, 2, answers); !errors.Is(err, userfeedback.ErrInvalidInput) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if err := userfeedback.ValidateFollowup(f, 4, ok); !errors.Is(err, userfeedback.ErrInvalidInput) {
		t.Errorf("高評価には追加質問が無い: err = %v", err)
	}
}

func TestNextEligibleAt(t *testing.T) {
	shown := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if got := userfeedback.NextEligibleAt(shown, 60); !got.Equal(time.Date(2026, 11, 30, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("next = %v", got)
	}
}
