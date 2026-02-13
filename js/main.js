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
            localStorage.setItem('username', username);
            window.location.href = 'articles.html';
        } else if (data.status === 2) {
            document.getElementById('errorMsg').innerText = 'ユーザー名かパスワードが間違っています。';
        } else {
            document.getElementById('errorMsg').innerText = 'ユーザーが存在しません。新規登録してください。';
        }
    });
}


// ---------- 新規登録処理 ----------
const signupForm = document.getElementById('signupForm');
if (signupForm) {
    signupForm.addEventListener('submit', (e) => {
        e.preventDefault();
        const newusername = document.getElementById('newusername').value;
        const newpassword = document.getElementById('newpassword').value;
        localStorage.setItem('temp_username', newusername);
        localStorage.setItem('temp_password', newpassword);
        window.location.href = 'tag-selection.html';
    });
}


// ---------- タグ選択処理 ----------
const tagForm = document.getElementById('tagForm');
if (tagForm) {
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

    // フォーム送信（バックエンドへのデータ送信）
    tagForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        const selectedTags = Array.from(tagCheckboxes)
            .filter(c => c.checked)
            .map(c => c.value);
        
        const newusername = localStorage.getItem('temp_username');
        const newpassword = localStorage.getItem('temp_password');

        if (!newusername || !newpassword) {
            alert('ユーザー情報が見つかりません。お手数ですが、もう一度最初から登録をやり直してください。');
            window.location.href = 'signup.html';
            return;
        }
        
        const res = await fetch(`${API_BASE_URL}/auth/create_user`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ 
                newusername: newusername, 
                newpassword: newpassword, 
                favoritetags: selectedTags 
            })
        });
        
        const data = await res.json();
        
        if (data.status === 1) {
            localStorage.removeItem('temp_username');
            localStorage.removeItem('temp_password');
            localStorage.setItem('username', newusername);
            window.location.href = 'articles.html';
        } else {
            alert('このユーザー名は既に使用されています。別のユーザー名で再度お試しください。');
            localStorage.removeItem('temp_username');
            localStorage.removeItem('temp_password');
            window.location.href = 'signup.html';
        }
    });
}


// ---------- 記事一覧表示処理 ----------
const articlesPageLogic = () => {
    const username = localStorage.getItem('username');
    if (!username) {
        window.location.href = 'index.html';
        return;
    }

    const qiitaContainer = document.getElementById('qiitaArticlesContainer');
    const zennContainer = document.getElementById('zennArticlesContainer');
    const tagsListContainer = document.getElementById('tagsList'); // サイドバーのUL要素を取得

    // 記事カードを作成してDOMに追加するヘルパー関数
    const createArticleCard = (article, container) => {
        const card = document.createElement('div');
        card.className = 'article-card';
        card.innerHTML = `
            <h3>${article.title}</h3>
            <p class="article-meta">
                <b>Source:</b> ${article.source} | 
                <b>Likes:</b> ${article.likes}
            </p>
            <p class="article-tags">
                <b>Tags:</b> ${article.tags.join(', ')}
            </p>
        `;
        card.addEventListener('click', () => {
            window.open(article.url, '_blank');
            fetch(`${API_BASE_URL}/article/click`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username, tags: article.tags })
            });
        });
        container.appendChild(card);
    };

    // サイドバーにタグリストを表示する関数を追加
    const displayUserTags = (tags) => {
        if (!tagsListContainer || !tags || tags.length === 0) return;

        tagsListContainer.innerHTML = ''; // 一旦中身を空にする
        tags.forEach(tag => {
            const listItem = document.createElement('li');
            listItem.textContent = tag;
            tagsListContainer.appendChild(listItem);
        });
    };

    // メインの処理: APIを呼び出して記事とタグを表示する
    const loadArticlesAndTags = async () => {
        try {
            const res = await fetch(`${API_BASE_URL}/news/personal_news`, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ username })
            });
            const data = await res.json();

            // バックエンドからのレスポンスに user_tags が含まれていると仮定
            // 例: { qiita_articles: [...], zenn_articles: [...], user_tags: ["Python", "React", "AWS"] }
            if (data.user_tags) {
                displayUserTags(data.user_tags);
            }

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