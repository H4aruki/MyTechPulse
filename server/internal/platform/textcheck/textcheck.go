// Package textcheck は、利用者が送る短い文字列(タグ・利用者名)に使える文字かを調べる。
// 制御文字・見えない文字・改行・タブは、どの欄でもはじく。
package textcheck

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxTagChars はタグの最大文字数。
	MaxTagChars = 50
	// MaxUsernameChars は利用者名の最大文字数。
	MaxUsernameChars = 50

	brailleBlank = '\U00002800'
)

// tagSymbols は文字・数字・空白のほかに、タグに使える記号。実際のタグ(C++、C#、CI/CD、#Next.js、自然言語処理（NLP）など)に合わせる。
const tagSymbols = ".+#-_/()（）&'・"

// hidden は、画面に何も出ない文字、または制御文字かを返す。
// ゼロ幅スペース(U+200B)・ZWNJ/ZWJ・U+2060・BOM(U+FEFF)・向きの制御(U+202A〜U+202E、U+2066〜U+2069)などの書式文字(Cf)、
// 制御文字(Cc)、未割り当て・私用の領域、異体字セレクタ、点字の空白、無視される文字を含む。
func hidden(r rune) bool {
	return r == utf8.RuneError ||
		!unicode.IsGraphic(r) ||
		r == brailleBlank ||
		unicode.Is(unicode.Other_Default_Ignorable_Code_Point, r) ||
		unicode.Is(unicode.Variation_Selector, r)
}

// HasHiddenRune は s に制御文字・見えない文字が含まれるか(または UTF-8 として不正か)を返す。
func HasHiddenRune(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if hidden(r) {
			return true
		}
	}
	return false
}

// ValidTag は、前後の空白を除いたタグが使える形かを返す。
// 1〜50文字で、文字・数字・半角の空白(続けて並べない)と tagSymbols だけを使える。
func ValidTag(tag string) bool {
	if !utf8.ValidString(tag) || tag != strings.TrimSpace(tag) {
		return false
	}
	n := utf8.RuneCountInString(tag)
	if n < 1 || n > MaxTagChars {
		return false
	}
	prev := rune(0)
	for _, r := range tag {
		switch {
		case hidden(r):
			return false
		case r == ' ':
			if prev == ' ' {
				return false
			}
		case unicode.IsLetter(r), unicode.IsNumber(r), strings.ContainsRune(tagSymbols, r):
		case unicode.IsMark(r):
			// 濁点などの結合文字は、直前に文字があるときだけ
			if prev == 0 || prev == ' ' {
				return false
			}
		default:
			return false
		}
		prev = r
	}
	return true
}

// ValidUsername は、前後の空白を除いた利用者名が使える形かを返す。
// 1〜50文字で、制御文字・見えない文字・改行・タブを含まない。
func ValidUsername(name string) bool {
	n := utf8.RuneCountInString(name)
	return n >= 1 && n <= MaxUsernameChars && !HasHiddenRune(name)
}
