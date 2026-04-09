// admin.js - Administration panel functionality

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

let currentLogLevels = {};

// Initialize admin panel
document.addEventListener('DOMContentLoaded', function() {
    loadLogLevels();
    loadCacheStats();
    loadProbeConfig();
    loadSystemInfo();
    
    // Auto-refresh every 30 seconds
    setInterval(() => {
        loadCacheStats();
        loadSystemInfo();
    }, 30000);
});

// Log Level Management
async function loadLogLevels() {
    try {
        showElement('log-levels-loading');
        hideElement('log-levels-container');
        
        const response = await fetch(`/api/${window.AGENT_KEY}/debug/logs`);
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        const data = await response.json();
        currentLogLevels = data.module_levels || {};
        
        renderLogModules();
        hideElement('log-levels-loading');
        showElement('log-levels-container');
    } catch (error) {
        console.error('Failed to load log levels:', error);
        hideElement('log-levels-loading');
        showError('Failed to load log levels: ' + error.message);
    }
}

function renderLogModules() {
    const grid = document.getElementById('log-modules-grid');
    grid.innerHTML = '';
    
    MODULES.forEach(module => {
        const moduleDiv = document.createElement('div');
        moduleDiv.className = 'log-module';
        
        const currentLevel = currentLogLevels[module] || 'info';
        
        moduleDiv.innerHTML = `
            <div class="log-module-name">${module}</div>
            <select class="log-level-select" data-module="${module}">
                ${LOG_LEVELS.map(level => 
                    `<option value="${level}" ${level === currentLevel ? 'selected' : ''}>${level.toUpperCase()}</option>`
                ).join('')}
            </select>
        `;
        
        grid.appendChild(moduleDiv);
    });
}

async function saveLogLevels() {
    try {
        const selects = document.querySelectorAll('.log-level-select');
        const moduleLevels = [];
        
        selects.forEach(select => {
            const module = select.getAttribute('data-module');
            const level = select.value;
            moduleLevels.push({ module, level });
        });
        
        const response = await fetch(`/api/${window.AGENT_KEY}/debug/logs`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify({ module_levels: moduleLevels })
        });
        
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        showSuccess('Log levels updated successfully!');
        // Reload to get the actual state from server
        setTimeout(() => loadLogLevels(), 1000);
    } catch (error) {
        console.error('Failed to save log levels:', error);
        showError('Failed to save log levels: ' + error.message);
    }
}

function resetLogLevels() {
    const selects = document.querySelectorAll('.log-level-select');
    selects.forEach(select => {
        select.value = 'info'; // Reset to default
    });
    showSuccess('Log levels reset to defaults. Click "Save Changes" to apply.');
}

// Cache Management
async function loadCacheStats() {
    try {
        const response = await fetch(`/api/${window.AGENT_KEY}/stats/cache`);
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        const data = await response.json();
        renderCacheStats(data);
    } catch (error) {
        console.error('Failed to load cache stats:', error);
        const tbody = document.getElementById('cache-stats-body');
        tbody.textContent = '';
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 4;
        td.style.textAlign = 'center';
        td.style.color = 'var(--danger-color)';
        td.textContent = 'Failed to load cache statistics: ' + error.message;
        tr.appendChild(td);
        tbody.appendChild(tr);
    }
}

function renderCacheStats(data) {
    const tbody = document.getElementById('cache-stats-body');
    
    if (!data || !data.probes || data.probes.length === 0) {
        tbody.textContent = '';
        const tr = document.createElement('tr');
        const td = document.createElement('td');
        td.colSpan = 4;
        td.style.textAlign = 'center';
        td.style.color = 'var(--gray-500)';
        td.textContent = 'No cache data available';
        tr.appendChild(td);
        tbody.appendChild(tr);
        return;
    }
    
    tbody.textContent = '';
    data.probes.forEach(probe => {
        const tr = document.createElement('tr');
        const tdName = document.createElement('td');
        tdName.textContent = probe.name;
        const tdCount = document.createElement('td');
        tdCount.textContent = parseInt(probe.metrics_count, 10) || 0;
        const tdUpdated = document.createElement('td');
        tdUpdated.textContent = probe.last_updated ? formatTimestamp(probe.last_updated) : 'Never';
        const tdStatus = document.createElement('td');
        const badge = document.createElement('span');
        badge.className = 'status-badge ' + (probe.metrics_count > 0 ? 'status-active' : 'status-inactive');
        badge.textContent = probe.metrics_count > 0 ? 'Active' : 'Empty';
        tdStatus.appendChild(badge);
        tr.appendChild(tdName);
        tr.appendChild(tdCount);
        tr.appendChild(tdUpdated);
        tr.appendChild(tdStatus);
        tbody.appendChild(tr);
    });
}

async function clearCache() {
    if (!confirm('Are you sure you want to clear the entire cache? This will remove all cached metrics.')) {
        return;
    }
    
    try {
        const response = await fetch(`/api/${window.AGENT_KEY}/admin/cache/clear`, {
            method: 'POST'
        });
        
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        showSuccess('Cache cleared successfully!');
        // Reload cache stats
        setTimeout(() => loadCacheStats(), 1000);
    } catch (error) {
        console.error('Failed to clear cache:', error);
        showError('Failed to clear cache: ' + error.message);
    }
}

// Probe Configuration
async function loadProbeConfig() {
    try {
        showElement('probe-config-loading');
        hideElement('probe-config-container');
        
        const response = await fetch(`/api/${window.AGENT_KEY}/config/probes`);
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        const data = await response.json();
        renderProbeConfig(data);
        hideElement('probe-config-loading');
        showElement('probe-config-container');
    } catch (error) {
        console.error('Failed to load probe config:', error);
        hideElement('probe-config-loading');
        const container = document.getElementById('probe-config-container');
        container.textContent = '';
        const errorDiv = document.createElement('div');
        errorDiv.style.textAlign = 'center';
        errorDiv.style.color = 'var(--danger-color)';
        errorDiv.style.padding = '2rem';
        errorDiv.textContent = 'Failed to load probe configuration: ' + error.message;
        container.appendChild(errorDiv);
        showElement('probe-config-container');
    }
}

function renderProbeConfig(data) {
    const container = document.getElementById('probe-config-container');
    
    if (!data || !data.probes || data.probes.length === 0) {
        container.textContent = '';
        const emptyDiv = document.createElement('div');
        emptyDiv.style.textAlign = 'center';
        emptyDiv.style.color = 'var(--gray-500)';
        emptyDiv.style.padding = '2rem';
        emptyDiv.textContent = 'No probe configuration found';
        container.appendChild(emptyDiv);
        return;
    }
    
    container.textContent = '';
    data.probes.forEach(probe => {
        const config = document.createElement('div');
        config.className = 'probe-config';
        const nameDiv = document.createElement('div');
        nameDiv.className = 'probe-name';
        nameDiv.textContent = probe.name;
        config.appendChild(nameDiv);

        const details = document.createElement('div');
        details.className = 'probe-details';

        const typeDiv = document.createElement('div');
        const typeLabel = document.createElement('strong');
        typeLabel.textContent = 'Type: ';
        typeDiv.appendChild(typeLabel);
        typeDiv.appendChild(document.createTextNode(probe.type || 'Unknown'));
        details.appendChild(typeDiv);

        const intervalDiv = document.createElement('div');
        const intervalLabel = document.createElement('strong');
        intervalLabel.textContent = 'Interval: ';
        intervalDiv.appendChild(intervalLabel);
        intervalDiv.appendChild(document.createTextNode(probe.interval || 'Default'));
        details.appendChild(intervalDiv);

        const enabledDiv = document.createElement('div');
        const enabledLabel = document.createElement('strong');
        enabledLabel.textContent = 'Enabled: ';
        enabledDiv.appendChild(enabledLabel);
        enabledDiv.appendChild(document.createTextNode(probe.enabled !== false ? 'Yes' : 'No'));
        details.appendChild(enabledDiv);

        if (probe.params) {
            const paramsDiv = document.createElement('div');
            const paramsLabel = document.createElement('strong');
            paramsLabel.textContent = 'Parameters: ';
            paramsDiv.appendChild(paramsLabel);
            const pre = document.createElement('pre');
            pre.textContent = JSON.stringify(probe.params, null, 2);
            paramsDiv.appendChild(pre);
            details.appendChild(paramsDiv);
        }

        config.appendChild(details);
        container.appendChild(config);
    });
}

// System Information
async function loadSystemInfo() {
    try {
        const response = await fetch(`/api/${window.AGENT_KEY}/health`);
        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }
        
        const data = await response.json();
        updateSystemInfo(data);
    } catch (error) {
        console.error('Failed to load system info:', error);
        // Set error values
        const elements = [
            'system-version', 'system-commit', 'system-go-version', 'system-os', 
            'system-arch', 'system-memory', 'system-goroutines', 'system-uptime'
        ];
        elements.forEach(id => {
            const element = document.getElementById(id);
            if (element) element.textContent = 'Error loading';
        });
    }
}

function updateSystemInfo(data) {
    // Update system information
    updateElement('system-version', data.version || 'Unknown');
    
    // Update commit information if available
    const commitRow = document.getElementById('system-commit-row');
    if (data.commit && data.commit.trim() !== '') {
        updateElement('system-commit', data.commit);
        commitRow.style.display = 'table-row';
    } else {
        commitRow.style.display = 'none';
    }
    
    updateElement('system-go-version', data.go_version || 'Unknown');
    updateElement('system-os', data.os || 'Unknown');
    updateElement('system-arch', data.arch || 'Unknown');
    updateElement('system-memory', formatBytes(data.memory_usage || 0));
    updateElement('system-goroutines', data.goroutines || 0);
    updateElement('system-uptime', formatDuration(data.uptime || 0));
}

// Utility Functions
function updateElement(id, value) {
    const element = document.getElementById(id);
    if (element) {
        element.textContent = value;
    }
}

function showElement(id) {
    const element = document.getElementById(id);
    if (element) {
        element.style.display = 'block';
    }
}

function hideElement(id) {
    const element = document.getElementById(id);
    if (element) {
        element.style.display = 'none';
    }
}

function showSuccess(message) {
    showMessage(message, 'success');
}

function showError(message) {
    showMessage(message, 'error');
}

function showMessage(message, type) {
    const messagesDiv = document.getElementById('messages');
    const messageDiv = document.createElement('div');
    messageDiv.className = `${type}-message`;
    const strong = document.createElement('strong');
    strong.textContent = type === 'success' ? 'Success: ' : 'Error: ';
    messageDiv.appendChild(strong);
    messageDiv.appendChild(document.createTextNode(message));

    messagesDiv.appendChild(messageDiv);
    
    // Auto-remove after 5 seconds
    setTimeout(() => {
        if (messageDiv.parentNode) {
            messageDiv.parentNode.removeChild(messageDiv);
        }
    }, 5000);
}

function formatBytes(bytes) {
    if (bytes === 0) return '0 Bytes';
    const k = 1024;
    const sizes = ['Bytes', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i];
}

function formatDuration(seconds) {
    if (seconds < 60) return `${seconds}s`;
    if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${seconds % 60}s`;
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    return `${hours}h ${minutes}m`;
}

function formatTimestamp(timestamp) {
    return new Date(timestamp).toLocaleString();
}