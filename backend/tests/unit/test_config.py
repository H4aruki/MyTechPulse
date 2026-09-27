from pathlib import Path

def test_settings_can_skip_dotenv_file(monkeypatch, tmp_path: Path) -> None:
    from backend.app.settings import load_settings

    sentinel_path = tmp_path / ".env"
    sentinel_path.write_text(
        "DATABASE_URL=postgresql+psycopg://sentinel-file-value/must-not-be-read\n"
        "QIITA_ACCESS_TOKEN=sentinel-file-token\n"
        "SECRET_KEY=sentinel-file-secret\n",
        encoding="utf-8",
    )
    monkeypatch.setenv("DATABASE_URL", "postgresql+psycopg://synthetic-environment/test")
    monkeypatch.setenv("QIITA_ACCESS_TOKEN", "synthetic-environment-token")
    monkeypatch.setenv("SECRET_KEY", "synthetic-environment-secret")

    settings = load_settings(dotenv_path=None)

    assert settings.DATABASE_URL == "postgresql+psycopg://synthetic-environment/test"
    assert settings.QIITA_ACCESS_TOKEN == "synthetic-environment-token"
    assert settings.SECRET_KEY == "synthetic-environment-secret"


def test_settings_read_explicit_dotenv_file_when_environment_is_absent(
    monkeypatch, tmp_path: Path
) -> None:
    from backend.app.settings import load_settings

    sentinel_path = tmp_path / ".env"
    sentinel_path.write_text(
        "DATABASE_URL=postgresql+psycopg://sentinel-file-value/must-not-be-read\n"
        "QIITA_ACCESS_TOKEN=sentinel-file-token\n"
        "SECRET_KEY=sentinel-file-secret\n",
        encoding="utf-8",
    )
    monkeypatch.delenv("DATABASE_URL", raising=False)
    monkeypatch.delenv("QIITA_ACCESS_TOKEN", raising=False)
    monkeypatch.delenv("SECRET_KEY", raising=False)

    settings = load_settings(dotenv_path=sentinel_path)

    assert settings.DATABASE_URL == "postgresql+psycopg://sentinel-file-value/must-not-be-read"
    assert settings.QIITA_ACCESS_TOKEN == "sentinel-file-token"
    assert settings.SECRET_KEY == "sentinel-file-secret"
