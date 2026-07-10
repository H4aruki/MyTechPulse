// mytechpulse-frontend/js/main.js

// グローバルな定数
const API_BASE_URL = 'http://127.0.0.1:8000';

// ---------- ログイン処理 ----------
const loginForm = document.getElementById('loginForm');
if (loginForm) {
    loginForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        const username = document.getElementById('username').value;
        const password = document.getElementById('password').value;
        const res = await fetch(`${API_BASE_URL}/auth/login_check`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ username, password })
        });
        const data = await res.json();
        if (data.status === 1) {
            localStorage.setItem('access_token', data.access_token);
            window.location.href = 'articles.html';
        } else {
            document.getElementById('errorMsg').innerText = 'ユーザー名またはパスワードが間違っています。';
        }
    });
}


// ---------- 新規登録処理（ユーザー情報入力→タグ選択を1画面内の2ステップで行い、1リクエストで登録を完結させる） ----------
const signupForm = document.getElementById('signupForm');
if (signupForm) {
    const signupStep1 = document.getElementById('signupStep1');
    const signupStep2 = document.getElementById('signupStep2');
    const signupNextBtn = document.getElementById('signupNextBtn');
    const newusernameInput = document.getElementById('newusername');
    const newpasswordInput = document.getElementById('newpassword');
    const signupErrorEl = document.getElementById('signupError');
    const tagCheckboxes = document.querySelectorAll('input[name="tags"]');
    const submitBtn = document.getElementById('submitBtn');
    const selectAllCheckboxes = document.querySelectorAll('.select-all');

    // 登録ボタンの有効/無効を切り替える関数
    const checkSubmitButton = () => {
        const anyChecked = Array.from(tagCheckboxes).some(box => box.checked);
        submitBtn.disabled = !anyChecked;
    };

    // カテゴリごとの一括選択
    selectAllCheckboxes.forEach(allBox => {
        allBox.addEventListener('change', (e) => {
            const category = e.target.dataset.category;
            const checkboxes = document.querySelectorAll(`.tags[data-category="${category}"] input[type="checkbox"]`);
            checkboxes.forEach(box => {
                box.checked = e.target.checked;
            });
            checkSubmitButton();
        });
    });

    // 個別チェックボックスのイベントリスナー
    tagCheckboxes.forEach(box => {
        box.addEventListener('change', checkSubmitButton);
    });

    // ステップ1 → ステップ2（Enterキーでの意図しない送信を防ぐため、通常のsubmitではなくボタンクリックで遷移）
    const goToStep2 = () => {
        if (!newusernameInput.value || !newpasswordInput.value) {
            signupErrorEl.innerText = 'ユーザー名とパスワードを入力してください。';
            return;
        }
        signupErrorEl.innerText = '';
        signupStep1.hidden = true;
        signupStep2.hidden = false;
    };
    signupNextBtn.addEventListener('click', goToStep2);
    newpasswordInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            e.preventDefault();
            goToStep2();
        }
    });

    // フォーム送信（ユーザー名・パスワード・タグをまとめて1リクエストで送信。パスワードをブラウザストレージへ一時保存しない）
    signupForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        const selectedTags = Array.from(tagCheckboxes)
            .filter(c => c.checked)
            .map(c => c.value);

        const res = await fetch(`${API_BASE_URL}/auth/create_user`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                newusername: newusernameInput.value,
                newpassword: newpasswordInput.value,
                favoritetags: selectedTags
            })
        });

        const data = await res.json();

        if (data.status === 1) {
            localStorage.setItem('access_token', data.access_token);
            window.location.href = 'articles.html';
        } else if (data.status === 2) {
            alert('このユーザー名は既に使用されています。別のユーザー名で再度お試しください。');
            signupStep2.hidden = true;
            signupStep1.hidden = false;
        } else {
            alert('登録中にエラーが発生しました。しばらくしてから再度お試しください。');
            signupStep2.hidden = true;
            signupStep1.hidden = false;
        }
    });
}


// ---------- ログアウト処理 ----------
const logoutBtn = document.getElementById('logoutBtn');
if (logoutBtn) {
    logoutBtn.addEventListener('click', () => {
        localStorage.removeItem('access_token');
    });
}


// ---------- 記事一覧表示処理 ----------
const articlesPageLogic = () => {
    const token = localStorage.getItem('access_token');
    if (!token) {
        window.location.href = 'index.html';
        return;
    }
    const authHeaders = {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`
    };

    // トークン切れ等で401が返った場合、トークンを破棄してログインページへ戻す
    const handleUnauthorized = (res) => {
        if (res.status === 401) {
            localStorage.removeItem('access_token');
            window.location.href = 'index.html';
            return true;
        }
        return false;
    };

    const qiitaContainer = document.getElementById('qiitaArticlesContainer');
    const zennContainer = document.getElementById('zennArticlesContainer');

    // Qiita/Zenn由来の外部データをinnerHTMLへ埋め込む前にエスケープする（XSS対策）
    const escapeHtml = (str) => {
        const div = document.createElement('div');
        div.textContent = String(str);
        return div.innerHTML;
    };

    // 記事カードを作成してDOMに追加するヘルパー関数
    const createArticleCard = (article, container) => {
        const card = document.createElement('div');
        card.className = 'article-card';
        card.innerHTML = `
            <h3>${escapeHtml(article.title)}</h3>
            <p class="article-meta">
                <b>Source:</b> ${escapeHtml(article.source)} |
                <b>Likes:</b> ${article.likes}
            </p>
            <p class="article-tags">
                <b>Tags:</b> ${article.tags.map(escapeHtml).join(', ')}
            </p>
        `;
        card.addEventListener('click', async () => {
            window.open(article.url, '_blank');
            const res = await fetch(`${API_BASE_URL}/article/click`, {
                method: 'POST',
                headers: authHeaders,
                body: JSON.stringify({ tags: article.tags })
            });
            handleUnauthorized(res);
        });
        container.appendChild(card);
    };

    // メインの処理: APIを呼び出して記事とタグを表示する
    const loadArticlesAndTags = async () => {
        try {
            const res = await fetch(`${API_BASE_URL}/news/personal_news`, {
                method: 'POST',
                headers: authHeaders
            });
            if (handleUnauthorized(res)) return;
            const data = await res.json();

            if (!qiitaContainer || !zennContainer) return;

            // Qiita
            qiitaContainer.innerHTML = '<h2>Qiita Articles</h2>';
            if (data.qiita_articles && data.qiita_articles.length > 0) {
                data.qiita_articles.forEach(article => createArticleCard(article, qiitaContainer));
            } else {
                qiitaContainer.innerHTML += '<p>おすすめのQiita記事はありません。</p>';
            }

            // Zenn
            zennContainer.innerHTML = '<h2>Zenn Articles</h2>';
            if (data.zenn_articles && data.zenn_articles.length > 0) {
                data.zenn_articles.forEach(article => createArticleCard(article, zennContainer));
            } else {
                zennContainer.innerHTML += '<p>おすすめのZenn記事はありません。</p>';
            }
        } catch (error) {
            console.error('記事の読み込みに失敗しました:', error);
            qiitaContainer.innerHTML = '<h2>記事の読み込みエラー</h2><p>記事の取得に失敗しました。時間をおいて再度お試しください。</p>';
            zennContainer.innerHTML = '';
        }
    };

    loadArticlesAndTags(); // 関数名を変更
};

// 現在のページに応じて適切な処理を実行
window.addEventListener('load', () => {
    if (document.getElementById('qiitaArticlesContainer')) { // articles.htmlのコンテナがあるかで判断
        articlesPageLogic();
    }
});