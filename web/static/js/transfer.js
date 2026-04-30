(function() {
    if (!document.getElementById('uploadSection')) return;

    var currentSection = 'upload';
    var currentUploadTab = 'uploading';
    var currentDownloadTab = 'downloading';
    window._activeDownloads = window._activeDownloads || {};
    var activeDownloads = window._activeDownloads;

    function removeTransferTask(id) {
        var tasks = getTransferTasks();
        var filtered = [];
        for (var i = 0; i < tasks.length; i++) {
            if (tasks[i].id !== id) filtered.push(tasks[i]);
        }
        saveTransferTasks(filtered);
    }

    function clearTransferTasks(type, status) {
        var tasks = getTransferTasks();
        var filtered = [];
        for (var i = 0; i < tasks.length; i++) {
            if (tasks[i].type !== type || tasks[i].status !== status) filtered.push(tasks[i]);
        }
        saveTransferTasks(filtered);
    }

    window.switchTransferSection = function(section) {
        currentSection = section;
        var links = document.querySelectorAll('.sub-sidebar-link');
        for (var i = 0; i < links.length; i++) {
            links[i].classList.toggle('active', links[i].getAttribute('data-section') === section);
        }
        var sections = document.querySelectorAll('.transfer-section');
        for (var j = 0; j < sections.length; j++) {
            sections[j].classList.toggle('active', sections[j].id === (section === 'upload' ? 'uploadSection' : 'downloadSection'));
        }
        renderAll();
    };

    function switchCategory(section, tab) {
        if (section === 'upload') {
            currentUploadTab = tab;
            var btns = document.querySelectorAll('#uploadCategoryBar .category-tab');
            for (var i = 0; i < btns.length; i++) {
                btns[i].classList.toggle('active', btns[i].getAttribute('data-tab') === tab);
            }
            var tabs = document.querySelectorAll('#uploadSection .transfer-tab-content');
            for (var j = 0; j < tabs.length; j++) {
                tabs[j].classList.toggle('active', tabs[j].id === (tab === 'uploading' ? 'uploadUploadingTab' : 'uploadCompletedTab'));
            }
        } else {
            currentDownloadTab = tab;
            var btns = document.querySelectorAll('#downloadCategoryBar .category-tab');
            for (var i = 0; i < btns.length; i++) {
                btns[i].classList.toggle('active', btns[i].getAttribute('data-tab') === tab);
            }
            var tabs = document.querySelectorAll('#downloadSection .transfer-tab-content');
            for (var j = 0; j < tabs.length; j++) {
                tabs[j].classList.toggle('active', tabs[j].id === (tab === 'downloading' ? 'downloadDownloadingTab' : 'downloadCompletedTab'));
            }
        }
        renderAll();
    }

    function setupCategoryBars() {
        var uploadBtns = document.querySelectorAll('#uploadCategoryBar .category-tab');
        for (var i = 0; i < uploadBtns.length; i++) {
            uploadBtns[i].addEventListener('click', function() {
                switchCategory('upload', this.getAttribute('data-tab'));
            });
        }
        var downloadBtns = document.querySelectorAll('#downloadCategoryBar .category-tab');
        for (var j = 0; j < downloadBtns.length; j++) {
            downloadBtns[j].addEventListener('click', function() {
                switchCategory('download', this.getAttribute('data-tab'));
            });
        }
    }

    function renderAll() {
        renderCounts();
        if (currentSection === 'upload') {
            renderUploadTable(currentUploadTab);
        } else {
            renderDownloadTable(currentDownloadTab);
        }
    }

    function renderCounts() {
        var tasks = getTransferTasks();
        var uploadActive = 0, uploadCompleted = 0, downloadActive = 0, downloadCompleted = 0;
        for (var i = 0; i < tasks.length; i++) {
            var t = tasks[i];
            if (t.type === 'upload') {
                if (t.status === 'active' || t.status === 'paused') uploadActive++;
                else if (t.status === 'completed') uploadCompleted++;
            } else {
                if (t.status === 'active' || t.status === 'paused') downloadActive++;
                else if (t.status === 'completed') downloadCompleted++;
            }
        }
        var elUploadUploading = document.getElementById('uploadUploadingCount');
        if (elUploadUploading) elUploadUploading.textContent = uploadActive;
        var elUploadCompleted = document.getElementById('uploadCompletedCount');
        if (elUploadCompleted) elUploadCompleted.textContent = uploadCompleted;
        var elDownloadDownloading = document.getElementById('downloadDownloadingCount');
        if (elDownloadDownloading) elDownloadDownloading.textContent = downloadActive;
        var elDownloadCompleted = document.getElementById('downloadCompletedCount');
        if (elDownloadCompleted) elDownloadCompleted.textContent = downloadCompleted;
    }

    function renderUploadTable(tab) {
        var tasks = getTransferTasks();
        var list;
        if (tab === 'uploading') {
            list = document.getElementById('uploadUploadingList');
        } else {
            list = document.getElementById('uploadCompletedList');
        }
        if (!list) return;

        var filtered = [];
        for (var i = 0; i < tasks.length; i++) {
            var t = tasks[i];
            if (t.type !== 'upload') continue;
            if (tab === 'uploading' && (t.status === 'active' || t.status === 'paused' || t.status === 'error')) {
                filtered.push(t);
            } else if (tab === 'uploadCompleted' && t.status === 'completed') {
                filtered.push(t);
            }
        }

        if (filtered.length === 0) {
            list.innerHTML = '<div class="empty-state">暂无记录</div>';
            return;
        }

        var html = '';
        for (var j = 0; j < filtered.length; j++) {
            var t = filtered[j];
            if (tab === 'uploading') {
                html += renderUploadingRow(t);
            } else {
                html += renderCompletedUploadRow(t);
            }
        }
        list.innerHTML = html;
    }

    function renderUploadingRow(t) {
        var statusHtml = '';
        if (t.status === 'error') {
            statusHtml = '<span class="status-badge status-error">失败</span>';
            if (t.error) statusHtml += ' <span style="font-size:11px;color:var(--danger)">' + escapeHtml(t.error) + '</span>';
        } else if (t.status === 'paused') {
            statusHtml = '<span class="status-badge status-paused">已暂停</span>';
            statusHtml += '<div class="transfer-progress-wrapper"><div class="transfer-progress-fill" style="width:' + (t.progress || 0) + '%"></div></div>';
            statusHtml += '<span class="transfer-progress-label">' + (t.progress || 0) + '%</span>';
        } else {
            statusHtml = '<span class="status-badge status-active">上传中</span>';
            statusHtml += '<div class="transfer-progress-wrapper"><div class="transfer-progress-fill" style="width:' + (t.progress || 0) + '%"></div></div>';
            statusHtml += '<span class="transfer-progress-label">' + (t.progress || 0) + '%</span>';
            if (t.speed > 0) {
                statusHtml += '<span class="transfer-speed">' + formatSize(t.speed) + '/s</span>';
            }
        }

        var actionHtml = '';
        if (t.status === 'active') {
            actionHtml = '<button class="action-btn" style="border-color:var(--primary);color:var(--primary);" onclick="pauseUpload(\'' + t.id + '\')">暂停</button>';
        } else if (t.status === 'paused') {
            actionHtml = '<button class="action-btn" style="border-color:var(--primary);color:var(--primary);" onclick="resumeUpload(\'' + t.id + '\')">开始</button>';
        }
        actionHtml += ' <button class="action-btn action-delperm" onclick="deleteTransferTask(\'' + t.id + '\')">删除</button>';

        return '<div class="file-row" data-task-id="' + t.id + '">' +
            '<div class="file-name">' + escapeHtml(t.name) + '</div>' +
            '<div class="file-actions">' + actionHtml + '</div>' +
            '<div class="file-size-cell">' + formatSize(t.size) + '</div>' +
            '<div style="flex:0 0 auto;min-width:180px;display:flex;align-items:center;gap:6px;font-size:13px;color:var(--text-secondary)">' + statusHtml + '</div>' +
            '</div>';
    }

    function renderCompletedUploadRow(t) {
        var completedTime = t.completedAt ? new Date(t.completedAt).toLocaleString() : '-';
        var actionHtml = '';
        if (t.fileId) {
            actionHtml += '<button class="action-btn" style="border-color:var(--primary);color:var(--primary);" onclick="viewFileLocation(' + t.fileId + ')">查看所在位置</button> ';
        }
        actionHtml += '<button class="action-btn action-delperm" onclick="deleteTransferTask(\'' + t.id + '\')">清除记录</button>';

        return '<div class="file-row" data-task-id="' + t.id + '">' +
            '<div class="file-name">' + escapeHtml(t.name) + '</div>' +
            '<div class="file-actions">' + actionHtml + '</div>' +
            '<div class="file-size-cell">' + formatSize(t.size) + '</div>' +
            '<div class="file-time-cell">' + completedTime + '</div>' +
            '</div>';
    }

    function renderDownloadTable(tab) {
        var tasks = getTransferTasks();
        var list;
        if (tab === 'downloading') {
            list = document.getElementById('downloadDownloadingList');
        } else {
            list = document.getElementById('downloadCompletedList');
        }
        if (!list) return;

        var filtered = [];
        for (var i = 0; i < tasks.length; i++) {
            var t = tasks[i];
            if (t.type !== 'download') continue;
            if (tab === 'downloading' && (t.status === 'active' || t.status === 'paused' || t.status === 'error')) {
                filtered.push(t);
            } else if (tab === 'downloadCompleted' && t.status === 'completed') {
                filtered.push(t);
            }
        }

        if (filtered.length === 0) {
            list.innerHTML = '<div class="empty-state">暂无记录</div>';
            return;
        }

        var html = '';
        for (var j = 0; j < filtered.length; j++) {
            var t = filtered[j];
            if (tab === 'downloading') {
                html += renderDownloadingRow(t);
            } else {
                html += renderCompletedDownloadRow(t);
            }
        }
        list.innerHTML = html;
    }

    function renderDownloadingRow(t) {
        var statusHtml = '';
        if (t.status === 'error') {
            statusHtml = '<span class="status-badge status-error">失败</span>';
            if (t.error) statusHtml += ' <span style="font-size:11px;color:var(--danger)">' + escapeHtml(t.error) + '</span>';
        } else if (t.status === 'paused') {
            statusHtml = '<span class="status-badge status-paused">已暂停</span>';
            statusHtml += '<div class="transfer-progress-wrapper"><div class="transfer-progress-fill" style="width:' + (t.progress || 0) + '%"></div></div>';
            statusHtml += '<span class="transfer-progress-label">' + (t.progress || 0) + '%</span>';
        } else {
            statusHtml = '<span class="status-badge status-active">下载中</span>';
            statusHtml += '<div class="transfer-progress-wrapper"><div class="transfer-progress-fill" style="width:' + (t.progress || 0) + '%"></div></div>';
            statusHtml += '<span class="transfer-progress-label">' + (t.progress || 0) + '%</span>';
            if (t.speed > 0) {
                statusHtml += '<span class="transfer-speed">' + formatSize(t.speed) + '/s</span>';
            }
        }

        var actionHtml = '';
        if (t.status === 'active') {
            actionHtml = '<button class="action-btn" style="border-color:var(--primary);color:var(--primary);" onclick="pauseDownload(\'' + t.id + '\')">暂停</button>';
        } else if (t.status === 'paused') {
            actionHtml = '<button class="action-btn" style="border-color:var(--primary);color:var(--primary);" onclick="resumeDownload(\'' + t.id + '\')">开始</button>';
        }
        actionHtml += ' <button class="action-btn action-delperm" onclick="deleteTransferTask(\'' + t.id + '\')">删除</button>';

        return '<div class="file-row" data-task-id="' + t.id + '">' +
            '<div class="file-name">' + escapeHtml(t.name) + '</div>' +
            '<div class="file-actions">' + actionHtml + '</div>' +
            '<div class="file-size-cell">' + formatSize(t.size) + '</div>' +
            '<div style="flex:0 0 auto;min-width:180px;display:flex;align-items:center;gap:6px;font-size:13px;color:var(--text-secondary)">' + statusHtml + '</div>' +
            '</div>';
    }

    function renderCompletedDownloadRow(t) {
        var completedTime = t.completedAt ? new Date(t.completedAt).toLocaleString() : '-';
        var actionHtml = '<button class="action-btn action-delperm" onclick="deleteTransferTask(\'' + t.id + '\')">清除记录</button>';

        return '<div class="file-row" data-task-id="' + t.id + '">' +
            '<div class="file-name">' + escapeHtml(t.name) + '</div>' +
            '<div class="file-actions">' + actionHtml + '</div>' +
            '<div class="file-size-cell">' + formatSize(t.size) + '</div>' +
            '<div class="file-time-cell">' + completedTime + '</div>' +
            '</div>';
    }

    window.startDownload = function(fileId, fileName, fileSize) {
        var taskId = 'download_' + fileId + '_' + Date.now();
        var task = {
            id: taskId,
            type: 'download',
            status: 'active',
            name: fileName,
            size: fileSize,
            fileId: fileId,
            receivedBytes: 0,
            progress: 0,
            speed: 0,
            completedAt: null,
            error: null
        };
        createTransferTask(task);
        renderAll();
        performDownload(taskId, fileId, fileName, fileSize);
    };

    function performDownload(taskId, fileId, fileName, fileSize, partialBlob, initialReceivedBytes) {
        _performDownload(taskId, fileId, fileName, fileSize, partialBlob || null, initialReceivedBytes || 0, function() {
            renderAll();
        });
    }

    window.pauseDownload = function(taskId) {
        updateTransferTask(taskId, { status: 'paused' });
        var ctrl = activeDownloads[taskId];
        if (ctrl) {
            ctrl.abort();
            if (ctrl.chunks && ctrl.chunks.length > 0) {
                var blob = new Blob(ctrl.chunks.slice());
                if (typeof _storeUploadFile === 'function') {
                    _storeUploadFile(taskId, blob).catch(function() {});
                }
            }
            delete activeDownloads[taskId];
        }
        renderAll();
    };

    window.resumeDownload = function(taskId) {
        var tasks = getTransferTasks();
        var task = null;
        for (var i = 0; i < tasks.length; i++) {
            if (tasks[i].id === taskId) { task = tasks[i]; break; }
        }
        if (!task) return;
        if (typeof _getUploadFile === 'function') {
            _getUploadFile(taskId).then(function(partialBlob) {
                var initialBytes = partialBlob ? partialBlob.size : 0;
                var pct = task.size > 0 ? Math.round((initialBytes / task.size) * 100) : 0;
                updateTransferTask(taskId, { status: 'active', progress: pct, receivedBytes: initialBytes, speed: 0 });
                renderAll();
                performDownload(taskId, task.fileId, task.name, task.size, partialBlob || null, initialBytes);
            }).catch(function() {
                updateTransferTask(taskId, { status: 'active', progress: 0, receivedBytes: 0, speed: 0 });
                renderAll();
                performDownload(taskId, task.fileId, task.name, task.size, null, 0);
            });
        } else {
            updateTransferTask(taskId, { status: 'active', progress: 0, receivedBytes: 0, speed: 0 });
            renderAll();
            performDownload(taskId, task.fileId, task.name, task.size, null, 0);
        }
    };

    window.pauseUpload = function(taskId) {
        updateTransferTask(taskId, { status: 'paused' });
        if (window._uploadControllers && window._uploadControllers[taskId]) {
            window._uploadControllers[taskId].pause();
        }
        renderAll();
    };

    window.resumeUpload = function(taskId) {
        var tasks = getTransferTasks();
        var task = null;
        for (var i = 0; i < tasks.length; i++) {
            if (tasks[i].id === taskId) { task = tasks[i]; break; }
        }
        if (!task) return;
        if (task.status === 'active') {
            if (window._uploadControllers && window._uploadControllers[taskId]) {
                window._uploadControllers[taskId].resume();
            }
            renderAll();
            return;
        }
        if (window._uploadControllers && window._uploadControllers[taskId]) {
            updateTransferTask(taskId, { status: 'active' });
            window._uploadControllers[taskId].resume();
            renderAll();
            return;
        }
        if (typeof resumeChunkedUpload === 'function') {
            resumeChunkedUpload(taskId);
        }
        renderAll();
    };

    window.deleteTransferTask = function(taskId) {
        var tasks = getTransferTasks();
        var task = null;
        for (var i = 0; i < tasks.length; i++) {
            if (tasks[i].id === taskId) { task = tasks[i]; break; }
        }
        if (task && task.type === 'upload' && task.uploadId && (task.status === 'active' || task.status === 'paused')) {
            apiFetch('/api/files/upload/cancel/' + task.uploadId, {
                method: 'DELETE',
                headers: { 'X-CSRF-Token': getCSRFToken() }
            }).catch(function() {});
        }
        if (task && task.type === 'download' && activeDownloads[taskId]) {
            activeDownloads[taskId].abort();
            delete activeDownloads[taskId];
        }
        if (window._uploadControllers && window._uploadControllers[taskId]) {
            window._uploadControllers[taskId].cancel();
            delete window._uploadControllers[taskId];
        }
        if (task && typeof _deleteUploadFile === 'function') {
            _deleteUploadFile(taskId).catch(function() {});
        }
        removeTransferTask(taskId);
        renderAll();
    };

    window.pauseAllUploads = function() {
        var tasks = getTransferTasks();
        for (var i = 0; i < tasks.length; i++) {
            if (tasks[i].type === 'upload' && tasks[i].status === 'active') {
                updateTransferTask(tasks[i].id, { status: 'paused' });
                if (window._uploadControllers && window._uploadControllers[tasks[i].id]) {
                    window._uploadControllers[tasks[i].id].pause();
                }
            }
        }
        renderAll();
    };

    window.resumeAllUploads = function() {
        var tasks = getTransferTasks();
        for (var i = 0; i < tasks.length; i++) {
            if (tasks[i].type === 'upload' && tasks[i].status === 'paused') {
                if (window._uploadControllers && window._uploadControllers[tasks[i].id]) {
                    updateTransferTask(tasks[i].id, { status: 'active' });
                    window._uploadControllers[tasks[i].id].resume();
                } else if (typeof resumeChunkedUpload === 'function') {
                    resumeChunkedUpload(tasks[i].id);
                }
            }
        }
        renderAll();
    };

    window.deleteAllUploads = function() {
        var tasks = getTransferTasks();
        for (var i = tasks.length - 1; i >= 0; i--) {
            var t = tasks[i];
            if (t.type === 'upload' && (t.status === 'active' || t.status === 'paused' || t.status === 'error')) {
                if (t.uploadId) {
                    apiFetch('/api/files/upload/cancel/' + t.uploadId, {
                        method: 'DELETE',
                        headers: { 'X-CSRF-Token': getCSRFToken() }
                    }).catch(function() {});
                }
                if (window._uploadControllers && window._uploadControllers[t.id]) {
                    window._uploadControllers[t.id].cancel();
                    delete window._uploadControllers[t.id];
                }
                if (typeof _deleteUploadFile === 'function') {
                    _deleteUploadFile(t.id).catch(function() {});
                }
                removeTransferTask(t.id);
            }
        }
        renderAll();
    };

    window.pauseAllDownloads = function() {
        var tasks = getTransferTasks();
        for (var i = 0; i < tasks.length; i++) {
            if (tasks[i].type === 'download' && tasks[i].status === 'active') {
                updateTransferTask(tasks[i].id, { status: 'paused' });
                var ctrl = activeDownloads[tasks[i].id];
                if (ctrl) {
                    ctrl.abort();
                    if (ctrl.chunks && ctrl.chunks.length > 0) {
                        var blob = new Blob(ctrl.chunks.slice());
                        if (typeof _storeUploadFile === 'function') {
                            _storeUploadFile(tasks[i].id, blob).catch(function() {});
                        }
                    }
                    delete activeDownloads[tasks[i].id];
                }
            }
        }
        renderAll();
    };

    window.resumeAllDownloads = function() {
        var tasks = getTransferTasks();
        var resumed = false;
        for (var i = 0; i < tasks.length; i++) {
            if (tasks[i].type === 'download' && tasks[i].status === 'paused') {
                window.resumeDownload(tasks[i].id);
                resumed = true;
            }
        }
        if (!resumed) renderAll();
    };

    window.deleteAllDownloads = function() {
        var tasks = getTransferTasks();
        for (var i = tasks.length - 1; i >= 0; i--) {
            var t = tasks[i];
            if (t.type === 'download' && (t.status === 'active' || t.status === 'paused' || t.status === 'error')) {
                if (activeDownloads[t.id]) {
                    activeDownloads[t.id].abort();
                    delete activeDownloads[t.id];
                }
                if (typeof _deleteUploadFile === 'function') {
                    _deleteUploadFile(t.id).catch(function() {});
                }
                removeTransferTask(t.id);
            }
        }
        renderAll();
    };

    window.clearCompletedUploads = function() {
        clearTransferTasks('upload', 'completed');
        renderAll();
    };

    window.clearCompletedDownloads = function() {
        clearTransferTasks('download', 'completed');
        renderAll();
    };

    window.viewFileLocation = function(fileId) {
        if (window.__routerNavigate) {
            window.__routerNavigate('/?highlight=' + fileId);
        } else {
            window.location.href = '/?highlight=' + fileId;
        }
    };

    function validateActiveUploads() {
        var tasks = getTransferTasks();
        var changed = false;
        var promises = [];
        for (var i = 0; i < tasks.length; i++) {
            if (tasks[i].type === 'upload' && (tasks[i].status === 'active' || tasks[i].status === 'paused') && tasks[i].uploadId) {
                (function(task) {
                    promises.push(
                        apiFetch('/api/files/upload/status/' + task.uploadId, {}).then(function(r) {
                            if (!r.ok) {
                                updateTransferTask(task.id, { status: 'error', error: '上传会话已过期' });
                                changed = true;
                            } else {
                                return r.json().then(function(data) {
                                    var pct = data.chunk_count > 0 ? Math.round((data.completed.length / data.chunk_count) * 100) : 0;
                                    updateTransferTask(task.id, { progress: pct });
                                });
                            }
                        }).catch(function() {
                            updateTransferTask(task.id, { status: 'error', error: '检查状态失败' });
                            changed = true;
                        })
                    );
                })(tasks[i]);
            }
        }
        return Promise.all(promises).then(function() { return changed; });
    }

    setupCategoryBars();

    validateActiveUploads().then(function() {
        renderAll();
        if (typeof autoResumeTransfers === 'function') {
            autoResumeTransfers();
        }
    });

    window._transferRefreshInterval = setInterval(function() {
        if (!document.getElementById('uploadSection')) {
            clearInterval(window._transferRefreshInterval);
            delete window._transferRefreshInterval;
            return;
        }
        if (document.visibilityState !== 'visible') return;
        var tasks = getTransferTasks();
        for (var i = 0; i < tasks.length; i++) {
            if (tasks[i].status === 'active') {
                renderAll();
                return;
            }
        }
    }, 1000);
})();
