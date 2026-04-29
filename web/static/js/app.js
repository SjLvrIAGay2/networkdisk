(function() {
    var dropOverlay = document.getElementById('dragOverlay');
    if (!dropOverlay) {
        dropOverlay = document.createElement('div');
        dropOverlay.id = 'dragOverlay';
        dropOverlay.className = 'drag-overlay';
        dropOverlay.textContent = '释放文件以上传';
        document.body.appendChild(dropOverlay);
    }

    var dragCounter = 0;
    document.addEventListener('dragenter', function(e) {
        e.preventDefault();
        dragCounter++;
        if (dragCounter === 1) dropOverlay.style.display = 'flex';
    });
    document.addEventListener('dragleave', function(e) {
        e.preventDefault();
        dragCounter--;
        if (dragCounter === 0) dropOverlay.style.display = 'none';
    });
    document.addEventListener('dragover', function(e) {
        e.preventDefault();
    });
    document.addEventListener('drop', function(e) {
        e.preventDefault();
        dragCounter = 0;
        dropOverlay.style.display = 'none';
        var files = e.dataTransfer.files;
        if (files.length > 0) {
            for (var i = 0; i < files.length; i++) {
                uploadFileChunked(files[i]);
            }
        }
    });

    document.addEventListener('keydown', function(e) {
        if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA' || e.target.tagName === 'SELECT') return;
        if ((e.ctrlKey || e.metaKey) && e.key === 'u') {
            e.preventDefault();
            var fileInput = document.getElementById('fileInput') || document.getElementById('quickUploadInput');
            if (fileInput) fileInput.click();
        }
        if (e.key === 'Delete' || e.key === 'Del') {
            e.preventDefault();
            batchDeleteSelected();
        }
        if (e.key === 'F2') {
            e.preventDefault();
            var selected = getSelectedFileIds();
            if (selected.length === 1) {
                var name = document.querySelector('[data-file-id="' + selected[0] + '"] .file-name');
                if (name) showRenameModal(selected[0], name.textContent.trim());
            }
        }
        if ((e.ctrlKey || e.metaKey) && e.key === 'a') {
            var fileList = document.querySelectorAll('[data-file-id]');
            if (fileList.length > 0) {
                e.preventDefault();
                fileList.forEach(function(el) { el.classList.add('selected'); });
            }
        }
        if ((e.ctrlKey || e.metaKey) && e.key === 'c') {
            e.preventDefault();
            var ids = getSelectedFileIds();
            if (ids.length > 0) {
                sessionStorage.setItem('clipboard_action', 'copy');
                sessionStorage.setItem('clipboard_ids', ids.join(','));
                showToast('已复制 ' + ids.length + ' 个文件');
            }
        }
        if ((e.ctrlKey || e.metaKey) && e.key === 'x') {
            e.preventDefault();
            var ids = getSelectedFileIds();
            if (ids.length > 0) {
                sessionStorage.setItem('clipboard_action', 'cut');
                sessionStorage.setItem('clipboard_ids', ids.join(','));
                showToast('已剪切 ' + ids.length + ' 个文件');
            }
        }
        if ((e.ctrlKey || e.metaKey) && e.key === 'v') {
            e.preventDefault();
            doPaste();
        }
    });
})();

function doPaste() {
    var action = sessionStorage.getItem('clipboard_action');
    var idsStr = sessionStorage.getItem('clipboard_ids');
    if (!idsStr) return;
    var ids = idsStr.split(',').map(function(s) { return parseInt(s, 10); });
    if (ids.length === 0) return;
    var targetDir = typeof currentDir !== 'undefined' ? currentDir : null;
    if (action === 'cut') {
        apiFetch('/api/files/batch-move', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': getCSRFToken() },
            body: JSON.stringify({ ids: ids, parent_id: targetDir })
        }).then(function(r) {
            if (r.ok) {
                sessionStorage.removeItem('clipboard_action');
                sessionStorage.removeItem('clipboard_ids');
                if (typeof refreshFiles === 'function') refreshFiles();
                showToast('粘贴成功');
            } else {
                showToast('粘贴失败');
            }
        }).catch(function() {
            showToast('粘贴失败');
        });
    }
}

function getSelectedFileIds() {
    var selected = [];
    var els = document.querySelectorAll('[data-file-id].selected');
    els.forEach(function(el) {
        var id = parseInt(el.getAttribute('data-file-id'), 10);
        if (id) selected.push(id);
    });
    return selected;
}

function batchDeleteSelected() {
    var ids = getSelectedFileIds();
    if (ids.length === 0) return;
    if (!confirm('确认删除 ' + ids.length + ' 个文件？')) return;
    apiFetch('/api/files/batch-delete', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': getCSRFToken() },
        body: JSON.stringify({ ids: ids })
    }).then(function(r) {
        if (r.ok) {
            if (typeof refreshFiles === 'function') refreshFiles();
            showToast('已删除 ' + ids.length + ' 个文件');
        } else {
            r.json().then(function(data) {
                showToast('删除失败: ' + (data.error || '未知错误'));
            }).catch(function() {
                showToast('删除失败');
            });
        }
    }).catch(function() {
        showToast('网络错误，请重试');
    });
}

var MAX_CHUNK_RETRIES = 3;
var TRANSFER_TASKS_KEY = 'transfer_tasks';

function getTransferTasks() {
    try { return JSON.parse(localStorage.getItem(TRANSFER_TASKS_KEY)) || []; }
    catch(e) { return []; }
}

function saveTransferTasks(tasks) {
    localStorage.setItem(TRANSFER_TASKS_KEY, JSON.stringify(tasks));
}

function createTransferTask(task) {
    var tasks = getTransferTasks();
    tasks.push(task);
    saveTransferTasks(tasks);
}

function updateTransferTask(id, updates) {
    var tasks = getTransferTasks();
    for (var i = 0; i < tasks.length; i++) {
        if (tasks[i].id === id) {
            for (var k in updates) {
                if (updates.hasOwnProperty(k)) tasks[i][k] = updates[k];
            }
            break;
        }
    }
    saveTransferTasks(tasks);
}

function uploadFileChunked(file) {
    var chunkSize = 10 * 1024 * 1024;
    var totalChunks = Math.ceil(file.size / chunkSize);
    var uploadID = null;
    var taskId = null;
    var paused = false;
    var index = 0;
    var progressDiv = createProgressItem(file.name);

    apiFetch('/api/files/upload/init', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': getCSRFToken() },
        body: JSON.stringify({ name: file.name, parent_id: typeof currentDir !== 'undefined' ? currentDir : null, total_size: file.size })
    }).then(function(r) {
        if (!r.ok) return r.json().then(function(d) { throw new Error(d.error || '初始化上传失败'); });
        return r.json();
    }).then(function(data) {
        uploadID = data.upload_id;
        taskId = 'upload_' + uploadID;
        if (data.chunk_size && data.chunk_size > 0) {
            chunkSize = data.chunk_size;
            totalChunks = Math.ceil(file.size / chunkSize);
        }
        createTransferTask({
            id: taskId,
            type: 'upload',
            status: 'active',
            name: file.name,
            size: file.size,
            uploadId: uploadID,
            fileId: null,
            parentId: typeof currentDir !== 'undefined' ? currentDir : null,
            progress: 0,
            speed: 0,
            completedAt: null,
            error: null
        });
        if (!window._uploadControllers) window._uploadControllers = {};
        window._uploadControllers[taskId] = {
            pause: function() { paused = true; },
            resume: function() { paused = false; uploadNext(); },
            cancel: function() { paused = true; index = totalChunks + 1; }
        };
        uploadNext();

        function uploadNext() {
            if (paused) return;
            if (index >= totalChunks) {
                completeUpload();
                return;
            }
            var start = index * chunkSize;
            var end = Math.min(start + chunkSize, file.size);
            var chunk = file.slice(start, end);
            uploadChunkWithRetry(uploadID, index, chunk, 0);
        }

        function uploadChunkWithRetry(uploadID, chunkIndex, chunk, retryCount) {
            if (paused) return;
            var form = new FormData();
            form.append('chunk', chunk);
            form.append('upload_id', uploadID);
            form.append('index', chunkIndex.toString());
            apiFetch('/api/files/upload/chunk', {
                method: 'POST',
                headers: { 'X-CSRF-Token': getCSRFToken() },
                body: form
            }).then(function(r) {
                if (!r.ok) return r.text().then(function(t) { throw new Error(t || '分片上传失败'); });
                index++;
                var pct = Math.round((index / totalChunks) * 100);
                try { updateProgress(progressDiv, pct); } catch(e) {}
                try { if (taskId) updateTransferTask(taskId, { progress: pct }); } catch(e) {}
                uploadNext();
            }).catch(function(e) {
                if (paused) return;
                if (retryCount < MAX_CHUNK_RETRIES) {
                    var d = 1000 * (retryCount + 1);
                    setTimeout(function() {
                        uploadChunkWithRetry(uploadID, chunkIndex, chunk, retryCount + 1);
                    }, d);
                } else {
                    try { removeProgress(progressDiv, e.message || '分片上传失败'); } catch(e2) {}
                    try { if (taskId) updateTransferTask(taskId, { status: 'error', error: e.message || '分片上传失败' }); } catch(e3) {}
                    if (taskId && window._uploadControllers) delete window._uploadControllers[taskId];
                }
            });
        }
    }).catch(function(e) {
        removeProgress(progressDiv, e.message);
    });

    function completeUpload() {
        apiFetch('/api/files/upload/complete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': getCSRFToken() },
            body: JSON.stringify({ upload_id: uploadID })
        }).then(function(r) {
            if (r.ok) {
                return r.json().then(function(data) {
                    removeProgress(progressDiv, null);
                    if (taskId) {
                        updateTransferTask(taskId, {
                            status: 'completed',
                            progress: 100,
                            fileId: data.id,
                            completedAt: new Date().toISOString()
                        });
                    }
                    if (taskId && window._uploadControllers) delete window._uploadControllers[taskId];
                    if (typeof refreshFiles === 'function') refreshFiles();
                    showToast('上传完成：' + file.name);
                });
            } else {
                removeProgress(progressDiv, '完成上传失败');
                if (taskId) updateTransferTask(taskId, { status: 'error', error: '完成上传失败' });
                if (taskId && window._uploadControllers) delete window._uploadControllers[taskId];
            }
        }).catch(function() {
            removeProgress(progressDiv, '完成上传失败');
            if (taskId) updateTransferTask(taskId, { status: 'error', error: '完成上传失败' });
            if (taskId && window._uploadControllers) delete window._uploadControllers[taskId];
        });
    }
}

var progressContainer = null;

function getProgressContainer() {
    if (!progressContainer) {
        progressContainer = document.createElement('div');
        progressContainer.id = 'uploadProgressContainer';
        progressContainer.style.cssText = 'position:fixed;bottom:24px;left:24px;width:320px;max-height:300px;overflow-y:auto;z-index:999;';
        document.body.appendChild(progressContainer);
    }
    return progressContainer;
}

function createProgressItem(name) {
    var container = getProgressContainer();
    var div = document.createElement('div');
    div.className = 'upload-progress-item';
    div.style.cssText = 'background:var(--surface);border:1px solid var(--border);border-radius:8px;padding:12px;margin-bottom:8px;';
    div.innerHTML = '<div class="name">' + escapeHtml(name) + '</div><div class="progress-bar-wrapper"><div class="progress-bar-fill" style="width:0%"></div></div><div style="font-size:12px;color:var(--text-secondary);margin-top:4px;">准备上传...</div>';
    container.appendChild(div);
    return div;
}

function updateProgress(div, pct) {
    var fill = div.querySelector('.progress-bar-fill');
    if (fill) fill.style.width = pct + '%';
    var label = div.querySelector('div:last-child');
    if (label) label.textContent = pct + '%';
}

function removeProgress(div, error) {
    if (error) {
        div.querySelector('.name').style.color = 'var(--danger)';
        div.querySelector('div:last-child').textContent = error;
        setTimeout(function() { if (div.parentNode) div.parentNode.removeChild(div); }, 3000);
    } else {
        setTimeout(function() { if (div.parentNode) div.parentNode.removeChild(div); }, 1500);
    }
}

function showToast(msg, type) {
    var toast = document.createElement('div');
    toast.className = 'toast';
    if (type === 'success') toast.className += ' toast-success';
    if (type === 'error') toast.className += ' toast-error';
    toast.textContent = msg;
    document.body.appendChild(toast);
    setTimeout(function() {
        if (toast.parentNode) toast.parentNode.removeChild(toast);
    }, 2500);
}

(function() {
    var btn = document.createElement('button');
    btn.className = 'mobile-menu-btn';
    btn.style.cssText = 'background:none;border:none;color:var(--text);font-size:20px;cursor:pointer;padding:4px 8px;';
    btn.innerHTML = '&#9776;';
    btn.onclick = function() {
        var sidebar = document.querySelector('.sidebar');
        if (sidebar) sidebar.classList.toggle('open');
    };
    var navbar = document.querySelector('.navbar-nav');
    if (navbar && window.innerWidth <= 768) {
        navbar.insertBefore(btn, navbar.firstChild);
    }
    window.addEventListener('resize', function() {
        if (window.innerWidth > 768 && btn.parentNode) {
            btn.parentNode.removeChild(btn);
        } else if (window.innerWidth <= 768 && navbar && !navbar.contains(btn)) {
            navbar.insertBefore(btn, navbar.firstChild);
        }
    });
})();
