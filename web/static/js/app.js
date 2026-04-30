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
var FILE_DB_NAME = 'nd_transfer_files';
var FILE_DB_VERSION = 1;
var FILE_STORE = 'blobs';

if (!window._activeDownloads) window._activeDownloads = {};

function _openFileDB() {
    if (typeof indexedDB === 'undefined') {
        return Promise.reject(new Error('IndexedDB不可用'));
    }
    return new Promise(function(resolve, reject) {
        var req = indexedDB.open(FILE_DB_NAME, FILE_DB_VERSION);
        req.onupgradeneeded = function(e) {
            if (!e.target.result.objectStoreNames.contains(FILE_STORE)) {
                e.target.result.createObjectStore(FILE_STORE);
            }
        };
        req.onsuccess = function(e) { resolve(e.target.result); };
        req.onerror = function(e) { reject(e.target.error); };
    });
}

function _storeUploadFile(key, file) {
    return _openFileDB().then(function(db) {
        return new Promise(function(resolve, reject) {
            var tx = db.transaction(FILE_STORE, 'readwrite');
            var store = tx.objectStore(FILE_STORE);
            var settled = false;
            tx.oncomplete = function() { if (!settled) { settled = true; db.close(); resolve(); } };
            tx.onerror = function() { if (!settled) { settled = true; db.close(); reject(tx.error); } };
            var req = store.put(file, key);
            req.onsuccess = function() {};
            req.onerror = function() { if (!settled) { settled = true; db.close(); reject(req.error); } };
        });
    });
}

function _getUploadFile(key) {
    return _openFileDB().then(function(db) {
        return new Promise(function(resolve, reject) {
            var tx = db.transaction(FILE_STORE, 'readonly');
            var store = tx.objectStore(FILE_STORE);
            var req = store.get(key);
            req.onsuccess = function() { db.close(); resolve(req.result || null); };
            req.onerror = function() { db.close(); reject(req.error); };
        });
    });
}

function _deleteUploadFile(key) {
    return _openFileDB().then(function(db) {
        return new Promise(function(resolve, reject) {
            var tx = db.transaction(FILE_STORE, 'readwrite');
            var store = tx.objectStore(FILE_STORE);
            var settled = false;
            tx.oncomplete = function() { if (!settled) { settled = true; db.close(); resolve(); } };
            tx.onerror = function() { if (!settled) { settled = true; db.close(); reject(tx.error); } };
            var req = store.delete(key);
            req.onsuccess = function() {};
            req.onerror = function() { if (!settled) { settled = true; db.close(); reject(req.error); } };
        });
    });
}

function _performDownload(taskId, fileId, fileName, fileSize, partialBlob, initialReceivedBytes, onChunk) {
    var aborted = false;
    var chunks = [];
    var receivedBytes = 0;
    var baseReceived = partialBlob ? partialBlob.size : 0;
    var lastTs = Date.now();
    var lastBytes = 0;

    window._activeDownloads = window._activeDownloads || {};
    window._activeDownloads[taskId] = {
        abort: function() { aborted = true; },
        chunks: chunks,
        receivedBytesCount: function() { return baseReceived + receivedBytes; }
    };

    function updateSpeed() {
        var now = Date.now();
        var elapsed = (now - lastTs) / 1000;
        if (elapsed >= 1) {
            var speed = (receivedBytes - lastBytes) / elapsed;
            lastTs = now;
            lastBytes = receivedBytes;
            updateTransferTask(taskId, { speed: speed > 0 ? Math.round(speed) : 0 });
        }
    }

    var fetchOptions = {};
    if (partialBlob && baseReceived > 0) {
        fetchOptions.headers = { 'Range': 'bytes=' + baseReceived + '-' };
    }

    apiFetch('/api/files/download/' + fileId, fetchOptions).then(function(r) {
        if (r.status !== 200 && r.status !== 206) {
            updateTransferTask(taskId, { status: 'error', error: '下载失败: ' + r.status });
            delete window._activeDownloads[taskId];
            _deleteUploadFile(taskId).catch(function() {});
            if (onChunk) onChunk();
            return;
        }

        if (partialBlob && baseReceived > 0 && r.status === 200) {
            partialBlob = null;
            baseReceived = 0;
            chunks = [];
        }

        var contentLength = parseInt(r.headers.get('Content-Length') || '0', 10);
        var reader = r.body.getReader();
        var totalSize = fileSize || contentLength || 1;

        function read() {
            if (aborted) {
                reader.cancel();
                return;
            }
            reader.read().then(function(result) {
                if (result.done) {
                    var allChunks = partialBlob ? [partialBlob].concat(chunks) : chunks;
                    var blob = new Blob(allChunks);
                    var url = window.URL.createObjectURL(blob);
                    var a = document.createElement('a');
                    a.href = url;
                    a.download = fileName;
                    document.body.appendChild(a);
                    a.click();
                    document.body.removeChild(a);
                    window.URL.revokeObjectURL(url);
                    updateTransferTask(taskId, {
                        status: 'completed',
                        progress: 100,
                        receivedBytes: baseReceived + receivedBytes,
                        completedAt: new Date().toISOString()
                    });
                    delete window._activeDownloads[taskId];
                    _deleteUploadFile(taskId).catch(function() {});
                    if (onChunk) onChunk();
                    if (typeof showToast === 'function') showToast('下载完成：' + fileName);
                    return;
                }
                chunks.push(result.value);
                receivedBytes += result.value.byteLength;
                var totalRcvd = baseReceived + receivedBytes;
                var pct = Math.round((totalRcvd / totalSize) * 100);
                if (pct > 100) pct = 99;
                updateTransferTask(taskId, { progress: pct, receivedBytes: totalRcvd });
                updateSpeed();
                if (onChunk) onChunk();
                read();
            }).catch(function(e) {
                if (!aborted) {
                    updateTransferTask(taskId, { status: 'error', error: e.message || '网络错误' });
                    delete window._activeDownloads[taskId];
                    _deleteUploadFile(taskId).catch(function() {});
                    if (onChunk) onChunk();
                }
            });
        }
        read();
    }).catch(function(e) {
        if (!aborted) {
            updateTransferTask(taskId, { status: 'error', error: e.message || '网络错误' });
            delete window._activeDownloads[taskId];
            _deleteUploadFile(taskId).catch(function() {});
            if (onChunk) onChunk();
        }
    });
}

window.resumeDownloadFromDB = function(taskId) {
    var tasks = getTransferTasks();
    var task = null;
    for (var i = 0; i < tasks.length; i++) {
        if (tasks[i].id === taskId) { task = tasks[i]; break; }
    }
    if (!task || !task.fileId || task.type !== 'download') return;
    if (task.status !== 'paused' && task.status !== 'error') return;

    _getUploadFile(taskId).then(function(partialBlob) {
        var initialBytes = partialBlob ? partialBlob.size : 0;
        var pct = task.size > 0 ? Math.round((initialBytes / task.size) * 100) : 0;
        updateTransferTask(taskId, { status: 'active', progress: pct, receivedBytes: initialBytes });
        _performDownload(taskId, task.fileId, task.name, task.size, partialBlob || null, initialBytes);
    }).catch(function() {
        updateTransferTask(taskId, { status: 'active', progress: 0, receivedBytes: 0 });
        _performDownload(taskId, task.fileId, task.name, task.size, null, 0);
    });
};

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

function uploadFileChunked(file, resumeOpts) {
    var uploadID = resumeOpts ? resumeOpts.uploadId : null;
    var taskId = resumeOpts ? resumeOpts.taskId : null;
    var chunkSize = resumeOpts && resumeOpts.chunkSize ? resumeOpts.chunkSize : 10 * 1024 * 1024;
    var totalChunks = Math.ceil(file.size / chunkSize);
    var paused = false;
    var index = resumeOpts && resumeOpts.startIndex !== undefined ? resumeOpts.startIndex : 0;
    var fileKey = taskId || ('_init_' + Date.now() + '_' + Math.random().toString(36).substr(2, 8));

    function registerController() {
        if (!window._uploadControllers) window._uploadControllers = {};
        window._uploadControllers[taskId] = {
            pause: function() { paused = true; },
            resume: function() { paused = false; uploadNext(); },
            cancel: function() { paused = true; index = totalChunks + 1; }
        };
    }

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

    function uploadChunkWithRetry(uid, chunkIndex, chunk, retryCount) {
        if (paused) return;
        var form = new FormData();
        form.append('chunk', chunk);
        form.append('upload_id', uid);
        form.append('index', chunkIndex.toString());
        apiFetch('/api/files/upload/chunk', {
            method: 'POST',
            headers: { 'X-CSRF-Token': getCSRFToken() },
            body: form
        }).then(function(r) {
            if (!r.ok) return r.text().then(function(t) { throw new Error(t || '分片上传失败'); });
            index++;
            var pct = Math.round((index / totalChunks) * 100);
            if (taskId) updateTransferTask(taskId, { progress: pct });
            uploadNext();
        }).catch(function(e) {
            if (paused) return;
            if (retryCount < MAX_CHUNK_RETRIES) {
                setTimeout(function() {
                    uploadChunkWithRetry(uid, chunkIndex, chunk, retryCount + 1);
                }, 1000 * (retryCount + 1));
            } else {
                if (taskId) updateTransferTask(taskId, { status: 'error', error: e.message || '分片上传失败' });
                if (taskId && window._uploadControllers) delete window._uploadControllers[taskId];
            }
        });
    }

    function completeUpload() {
        apiFetch('/api/files/upload/complete', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': getCSRFToken() },
            body: JSON.stringify({ upload_id: uploadID })
        }).then(function(r) {
            if (r.ok) {
                return r.json().then(function(data) {
                    if (taskId) {
                        updateTransferTask(taskId, {
                            status: 'completed',
                            progress: 100,
                            fileId: data.id,
                            completedAt: new Date().toISOString()
                        });
                    }
                    if (taskId && window._uploadControllers) delete window._uploadControllers[taskId];
                    _deleteUploadFile(fileKey).catch(function() {});
                    if (typeof refreshFiles === 'function') refreshFiles();
                    showToast('上传完成：' + file.name);
                });
            } else {
                if (taskId) updateTransferTask(taskId, { status: 'error', error: '完成上传失败' });
                if (taskId && window._uploadControllers) delete window._uploadControllers[taskId];
            }
        }).catch(function() {
            if (taskId) updateTransferTask(taskId, { status: 'error', error: '完成上传失败' });
            if (taskId && window._uploadControllers) delete window._uploadControllers[taskId];
        });
    }

    _storeUploadFile(fileKey, file).catch(function() {});
    if (resumeOpts) {
        if (taskId) updateTransferTask(taskId, { status: 'active', error: null });
        registerController();
        uploadNext();
        return;
    }
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
        var newKey = taskId;
        _storeUploadFile(newKey, file).then(function() {
            if (newKey !== fileKey) _deleteUploadFile(fileKey).catch(function() {});
        }).catch(function() {});
        fileKey = newKey;
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
        registerController();
        uploadNext();
    }).catch(function(e) {
        _deleteUploadFile(fileKey).catch(function() {});
    });
}

function resumeChunkedUpload(taskId) {
    var tasks = getTransferTasks();
    var task = null;
    for (var i = 0; i < tasks.length; i++) {
        if (tasks[i].id === taskId) { task = tasks[i]; break; }
    }
    if (!task || !task.uploadId || task.type !== 'upload') return;
    if (task.status !== 'paused' && task.status !== 'active' && task.status !== 'error') return;

    if (window._uploadControllers && window._uploadControllers[taskId]) {
        if (task.status !== 'active') updateTransferTask(taskId, { status: 'active' });
        window._uploadControllers[taskId].resume();
        return;
    }

    if (!window._pendingResume) window._pendingResume = {};
    if (window._pendingResume[taskId]) return;
    window._pendingResume[taskId] = true;

    updateTransferTask(taskId, { status: 'active', error: null });

    _getUploadFile(taskId).then(function(file) {
        if (!file) {
            updateTransferTask(taskId, { status: 'error', error: '文件数据已过期，请重新选择文件上传' });
            delete window._pendingResume[taskId];
            return;
        }
        apiFetch('/api/files/upload/status/' + task.uploadId, {}).then(function(r) {
            if (!r.ok) {
                updateTransferTask(taskId, { status: 'error', error: '上传会话已过期' });
                _deleteUploadFile(taskId).catch(function() {});
                delete window._pendingResume[taskId];
                return;
            }
            return r.json();
        }).then(function(data) {
            if (!data) { delete window._pendingResume[taskId]; return; }
            var completedSet = {};
            for (var j = 0; j < data.completed.length; j++) {
                completedSet[data.completed[j]] = true;
            }
            var nextIndex = 0;
            while (completedSet[nextIndex]) { nextIndex++; }
            delete window._pendingResume[taskId];
            uploadFileChunked(file, {
                uploadId: task.uploadId,
                taskId: taskId,
                chunkSize: data.chunk_size,
                startIndex: nextIndex
            });
        }).catch(function() {
            updateTransferTask(taskId, { status: 'error', error: '无法恢复上传' });
            _deleteUploadFile(taskId).catch(function() {});
            delete window._pendingResume[taskId];
        });
    });
}

window.addEventListener('pagehide', function() {
    var tasks = getTransferTasks();
    for (var i = 0; i < tasks.length; i++) {
        if (tasks[i].status === 'active') {
            updateTransferTask(tasks[i].id, { status: 'paused' });
            if (window._uploadControllers && window._uploadControllers[tasks[i].id]) {
                window._uploadControllers[tasks[i].id].pause();
            }
        }
    }
    for (var i = 0; i < tasks.length; i++) {
        if (tasks[i].type === 'download' && tasks[i].status === 'active') {
            var ctrl = window._activeDownloads && window._activeDownloads[tasks[i].id];
            if (ctrl) {
                ctrl.abort();
                if (ctrl.chunks && ctrl.chunks.length > 0) {
                    var blob = new Blob(ctrl.chunks.slice());
                    _storeUploadFile(tasks[i].id, blob).catch(function() {});
                }
                updateTransferTask(tasks[i].id, { status: 'paused' });
                delete window._activeDownloads[tasks[i].id];
            }
        }
    }
});

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

function autoResumeTransfers() {
    var tasks = getTransferTasks();
    for (var i = 0; i < tasks.length; i++) {
        if (tasks[i].type === 'upload' && tasks[i].uploadId) {
            var orphaned = tasks[i].status === 'active' && (!window._uploadControllers || !window._uploadControllers[tasks[i].id]);
            if (tasks[i].status === 'paused' || tasks[i].status === 'error' || orphaned) {
                if (typeof resumeChunkedUpload === 'function') {
                    resumeChunkedUpload(tasks[i].id);
                }
            }
        }
        if (tasks[i].type === 'download' && tasks[i].status === 'paused' && tasks[i].fileId) {
            if (typeof window.resumeDownloadFromDB === 'function') {
                window.resumeDownloadFromDB(tasks[i].id);
            }
        }
    }
}

(function() {
    autoResumeTransfers();
})();

window.addEventListener('pageshow', function(e) {
    if (!e.persisted) return;
    autoResumeTransfers();
});
