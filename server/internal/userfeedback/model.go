// Package userfeedback は利用者フィードバック（アンケート）の表示判定・回答の保存を扱う。
// 設計: docs/superpowers/specs/2026-09-10-user-feedback-design.md
package userfeedback

import (
	"errors"
	"time"
)

var (
	ErrNotFound          = errors.New("userfeedback: not found")
	ErrInvalidInput      = errors.New("userfeedback: invalid input")
	ErrConflict          = errors.New("userfeedback: conflict")
	ErrInvalidDefinition = errors.New("userfeedback: invalid form definition")
)

const (
	FollowupScoreMax int32 = 2
	MinScore         int32 = 1
	MaxScore         int32 = 5
)

type PromptStatus string

const (
	PromptShown     PromptStatus = "shown"
	PromptDismissed PromptStatus = "dismissed"
	PromptSubmitted PromptStatus = "submitted"
)

type Stage string

const (
	StageOverall  Stage = "overall"
	StageFollowup Stage = "followup"
)

type SubmissionStatus string

const (
	SubmissionPartial   SubmissionStatus = "partial"
	SubmissionCompleted SubmissionStatus = "completed"
)

type Question struct {
	ID                  int64
	Key                 string
	Text                string
	SortOrder           int32
	Required            bool
	DisplayIfQuestionID *int64
	DisplayIfScoreMax   *int32
}

type Form struct {
	ID           int64
	Key          string
	Version      int32
	Title        string
	CooldownDays int32
	Questions    []Question
}

type Answer struct {
	QuestionID int64
	Score      int32
}

type PromptSummary struct {
	ID               string
	Status           PromptStatus
	Stage            Stage
	ShownAt          time.Time
	SubmissionID     *string
	SubmissionStatus *SubmissionStatus
}

type Status struct {
	Eligible       bool
	NextEligibleAt *time.Time
	LastPrompt     *PromptSummary
}

type Presentation struct {
	Eligible       bool
	NextEligibleAt *time.Time
	Prompt         *PromptSummary
	Form           *Form
}

type SubmitResult struct {
	SubmissionID     string
	Status           SubmissionStatus
	FollowupRequired bool
}
