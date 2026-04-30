(function() {
    'use strict';

    var token = localStorage.getItem('access_token');
    if (!token) {
        window.location.replace('/login');
        return;
    }

    apiFetch('/api/auth/me', {}).then(function(r) {
        if (!r.ok) {
            localStorage.removeItem('access_token');
            window.location.replace('/login');
            throw new Error('auth failed');
        }
        return r.json();
    }).then(function(data) {
        if (data && data.username) {
            var el = document.getElementById('navUsername');
            if (el) el.textContent = data.username;
        }
        initRouter();
    }).catch(function() {
        localStorage.removeItem('access_token');
        window.location.replace('/login');
    });

    function initRouter() {
        setActiveSidebar(window.location.pathname);
        setupClickInterception();
        window.addEventListener('popstate', onPopState);
    }

    function setActiveSidebar(pathname) {
        var links = document.querySelectorAll('.sidebar a[data-route]');
        for (var i = 0; i < links.length; i++) {
            var route = links[i].getAttribute('data-route');
            if (route === pathname) {
                links[i].classList.add('active');
            } else {
                links[i].classList.remove('active');
            }
        }
    }

    function setupClickInterception() {
        document.addEventListener('click', function(e) {
            var link = e.target.closest('a[data-spa]');
            if (!link) return;
            var href = link.getAttribute('href');
            if (!href) return;
            if (link.target === '_blank') return;
            e.preventDefault();
            navigate(href);
        });
    }

    function cleanupBeforeNavigate() {
        if (window._transferRefreshInterval) {
            clearInterval(window._transferRefreshInterval);
            delete window._transferRefreshInterval;
        }
    }

    function navigate(url) {
        var parser = document.createElement('a');
        parser.href = url;
        var path = parser.pathname;
        if (path !== window.location.pathname || parser.search !== window.location.search) {
            history.pushState(null, '', url);
        }
        cleanupBeforeNavigate();
        fetchPartial(url, path);
    }

    function fetchPartial(url, path) {
        fetch(url, {
            headers: { 'X-SPA-Partial': 'true', 'Accept': 'application/json' },
            credentials: 'same-origin'
        }).then(function(r) {
            if (r.status === 401) {
                localStorage.removeItem('access_token');
                window.location.replace('/login');
                return;
            }
            if (!r.ok) throw new Error('partial fetch failed: ' + r.status);
            return r.json();
        }).then(function(data) {
            if (!data) return;
            applyPartial(data, path);
        }).catch(function() {
            window.location.href = url;
        });
    }

    function applyPartial(data, path) {
        document.title = data.title;
        document.getElementById('mainContent').innerHTML = data.content;
        setActiveSidebar(path);
        var mainEl = document.getElementById('mainContent');
        if (path === '/transfer') {
            mainEl.classList.add('with-sub-sidebar');
        } else {
            mainEl.classList.remove('with-sub-sidebar');
        }
        executeScripts(data.scripts);
    }

    function executeScripts(scriptsHtml) {
        if (!scriptsHtml) return;
        var temp = document.createElement('div');
        temp.innerHTML = scriptsHtml;
        var scripts = temp.querySelectorAll('script');
        for (var i = 0; i < scripts.length; i++) {
            var oldScript = scripts[i];
            var newScript = document.createElement('script');
            if (oldScript.src) {
                newScript.src = oldScript.src;
                newScript.async = false;
                document.body.appendChild(newScript);
                document.body.removeChild(newScript);
            } else {
                newScript.textContent = oldScript.textContent;
                document.body.appendChild(newScript);
                document.body.removeChild(newScript);
            }
        }
    }

    function onPopState() {
        cleanupBeforeNavigate();
        fetchPartial(window.location.href, window.location.pathname);
    }

    window.__routerNavigate = navigate;
})();
