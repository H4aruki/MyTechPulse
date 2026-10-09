package userfeedback

import (
	"fmt"
	"time"
)

// ValidateDefinition は、DB制約だけでは守れない、複数の質問にまたがる条件を確かめる。
func ValidateDefinition(f Form) error {
	if f.CooldownDays < 1 {
		return fmt.Errorf("%w: 再表示間隔は1日以上にしてください", ErrInvalidDefinition)
	}
	keys := map[string]bool{}
	orders := map[int32]bool{}
	var roots []Question
	for _, q := range f.Questions {
		if keys[q.Key] || orders[q.SortOrder] {
			return fmt.Errorf("%w: 質問キーまたは並び順が重複しています", ErrInvalidDefinition)
		}
		keys[q.Key], orders[q.SortOrder] = true, true
		if q.DisplayIfQuestionID == nil {
			if q.DisplayIfScoreMax != nil {
				return fmt.Errorf("%w: 表示条件の参照先が無いのにしきい値があります", ErrInvalidDefinition)
			}
			roots = append(roots, q)
		}
	}
	if len(roots) != 1 {
		return fmt.Errorf("%w: 総合評価はちょうど1問にしてください", ErrInvalidDefinition)
	}
	root := roots[0]
	for _, q := range f.Questions {
		if q.DisplayIfQuestionID == nil {
			continue
		}
		if *q.DisplayIfQuestionID != root.ID || q.DisplayIfScoreMax == nil || *q.DisplayIfScoreMax != FollowupScoreMax {
			return fmt.Errorf("%w: 追加質問は総合評価だけを参照し、しきい値を2にしてください", ErrInvalidDefinition)
		}
	}
	return nil
}

func (f Form) Root() Question {
	for _, q := range f.Questions {
		if q.DisplayIfQuestionID == nil {
			return q
		}
	}
	return Question{}
}

func (f Form) FollowupsFor(score int32) []Question {
	var out []Question
	for _, q := range f.Questions {
		if q.DisplayIfScoreMax != nil && score <= *q.DisplayIfScoreMax {
			out = append(out, q)
		}
	}
	return out
}

func validScore(score int32) bool { return score >= MinScore && score <= MaxScore }

func ValidateOverall(f Form, questionID int64, score int32) (bool, error) {
	if !validScore(score) {
		return false, fmt.Errorf("%w: 評価は1〜5で選んでください", ErrInvalidInput)
	}
	if f.Root().ID != questionID {
		return false, fmt.Errorf("%w: 総合評価の質問ではありません", ErrInvalidInput)
	}
	return len(f.FollowupsFor(score)) > 0, nil
}

func ValidateFollowup(f Form, overallScore int32, answers []Answer) error {
	expected := f.FollowupsFor(overallScore)
	if len(expected) == 0 {
		return fmt.Errorf("%w: 追加質問はありません", ErrInvalidInput)
	}
	byID := make(map[int64]Question, len(expected))
	for _, q := range expected {
		byID[q.ID] = q
	}
	seen := map[int64]bool{}
	for _, a := range answers {
		if _, ok := byID[a.QuestionID]; !ok || seen[a.QuestionID] || !validScore(a.Score) {
			return fmt.Errorf("%w: 回答の内容が正しくありません", ErrInvalidInput)
		}
		seen[a.QuestionID] = true
	}
	for _, q := range expected {
		if q.Required && !seen[q.ID] {
			return fmt.Errorf("%w: すべての質問に回答してください", ErrInvalidInput)
		}
	}
	return nil
}

func NextEligibleAt(lastShown time.Time, cooldownDays int32) time.Time {
	return lastShown.AddDate(0, 0, int(cooldownDays))
}
