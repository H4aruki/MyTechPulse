package config

import (
	"strings"
	"testing"
	"time"
)

func TestArticleFetchIsDisabledByDefaultInEveryEnvironment(t *testing.T) {
	for name, values := range map[string]map[string]string{
		"local":      validValues(),
		"production": productionValues(),
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(mapLookup(values))
			if err != nil {
				t.Fatal(err)
			}
			af := cfg.ArticleFetch
			if af.Enabled || af.QiitaMaxRequests != 300 || af.RunTimeout != 20*time.Minute {
				t.Fatalf("ArticleFetch = %+v", af)
			}
		})
	}
	test := validValues()
	test["APP_ENV"] = "test"
	if cfg, err := Load(mapLookup(test)); err != nil || cfg.ArticleFetch.Enabled {
		t.Fatalf("test環境: cfg = %+v, err = %v", cfg.ArticleFetch, err)
	}
}

func TestArticleFetchReadsValues(t *testing.T) {
	values := productionValues()
	values["ARTICLE_FETCH_ENABLED"] = "true"
	values["ARTICLE_FETCH_QIITA_MAX_REQUESTS"] = "800"
	values["ARTICLE_FETCH_RUN_TIMEOUT"] = "30m"
	cfg, err := Load(mapLookup(values))
	if err != nil {
		t.Fatal(err)
	}
	af := cfg.ArticleFetch
	if !af.Enabled || af.QiitaMaxRequests != 800 || af.RunTimeout != 30*time.Minute {
		t.Fatalf("ArticleFetch = %+v", af)
	}
}

func TestArticleFetchRejectsInvalidValues(t *testing.T) {
	for name, kv := range map[string][2]string{
		"真偽値が不正":       {"ARTICLE_FETCH_ENABLED", "yes please"},
		"上限が0":         {"ARTICLE_FETCH_QIITA_MAX_REQUESTS", "0"},
		"上限が負":         {"ARTICLE_FETCH_QIITA_MAX_REQUESTS", "-1"},
		"上限が801":       {"ARTICLE_FETCH_QIITA_MAX_REQUESTS", "801"},
		"上限が数字でない":     {"ARTICLE_FETCH_QIITA_MAX_REQUESTS", "many"},
		"実行の上限が0":      {"ARTICLE_FETCH_RUN_TIMEOUT", "0s"},
		"実行の上限が不正な書き方": {"ARTICLE_FETCH_RUN_TIMEOUT", "twenty"},
	} {
		t.Run(name, func(t *testing.T) {
			values := validValues()
			values[kv[0]] = kv[1]
			_, err := Load(mapLookup(values))
			if err == nil {
				t.Fatal("起動エラーにならない")
			}
			if len(kv[1]) > 3 && strings.Contains(err.Error(), kv[1]) {
				t.Fatalf("エラーに入力値が含まれている: %q", err.Error())
			}
		})
	}
}
