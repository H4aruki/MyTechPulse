// Package articlefetch は、QiitaとZennの記事を決まった時刻に一括で取得し、DBへ貯める（Issue #66 ステップ1）。
// フィードはまだこの結果を読まない。定時は日本時間の 6:00 / 11:30 / 16:30。
package articlefetch

import "time"

type slotTime struct{ hour, minute int }

// slotTimes は1日の定時（日本時間）。昇順に並べる。
var slotTimes = []slotTime{{6, 0}, {11, 30}, {16, 30}}

// Location は定時を数えるタイムゾーン(Asia/Tokyo)。
// 本番の箱にタイムゾーンの情報が無くても動くよう、読めなければ固定の+9時間を使う（日本は夏時間が無いので同じ結果）。
func Location() *time.Location {
	if loc, err := time.LoadLocation("Asia/Tokyo"); err == nil {
		return loc
	}
	return time.FixedZone("JST", 9*60*60)
}

func slotOn(t time.Time, s slotTime, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, s.hour, s.minute, 0, 0, loc)
}

// LatestSlot は now 以下で最も新しい定時を返す。
func LatestSlot(now time.Time, loc *time.Location) time.Time {
	for i := len(slotTimes) - 1; i >= 0; i-- {
		if s := slotOn(now, slotTimes[i], loc); !s.After(now) {
			return s
		}
	}
	// 6:00より前は、前日の最後の定時
	return slotOn(now.In(loc).AddDate(0, 0, -1), slotTimes[len(slotTimes)-1], loc)
}

// NextSlot は now より後で最初の定時を返す（now がちょうど定時なら、次の定時）。
func NextSlot(now time.Time, loc *time.Location) time.Time {
	for _, st := range slotTimes {
		if s := slotOn(now, st, loc); s.After(now) {
			return s
		}
	}
	return slotOn(now.In(loc).AddDate(0, 0, 1), slotTimes[0], loc)
}
