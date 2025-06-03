// admin.js - Administration panel functionality

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
        tbody.innerHTML = `
            <tr>
                <td colspan="4" style="text-align: center; color: var(--danger-color);">
                    Failed to load cache statistics: ${error.message}
                </td>
            </tr>
        `;
    }
}

function renderCacheStats(data) {
    const tbody = document.getElementById('cache-stats-body');
    
    if (!data || !data.probes || data.probes.length === 0) {
        tbody.innerHTML = `
            <tr>
                <td colspan="4" style="text-align: center; color: var(--gray-500);">
                    No cache data available
                </td>
            </tr>
        `;
        return;
    }
    
    tbody.innerHTML = data.probes.map(probe => `
        <tr>
            <td>${probe.name}</td>
            <td>${probe.metrics_count || 0}</td>
            <td>${probe.last_updated ? formatTimestamp(probe.last_updated) : 'Never'}</td>
            <td>
                <span class="status-badge ${probe.metrics_count > 0 ? 'status-active' : 'status-inactive'}">
                    ${probe.metrics_count > 0 ? 'Active' : 'Empty'}
                </span>
            </td>
        </tr>
    `).join('');
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
        container.innerHTML = `
            <div style="text-align: center; color: var(--danger-color); padding: 2rem;">
                Failed to load probe configuration: ${error.message}
            </div>
        `;
        showElement('probe-config-container');
    }
}

function renderProbeConfig(data) {
    const container = document.getElementById('probe-config-container');
    
    if (!data || !data.probes || data.probes.length === 0) {
        container.innerHTML = `
            <div style="text-align: center; color: var(--gray-500); padding: 2rem;">
                No probe configuration found
            </div>
        `;
        return;
    }
    
    container.innerHTML = data.probes.map(probe => `
        <div class="probe-config">
            <div class="probe-name">${probe.name}</div>
            <div class="probe-details">
                <div><strong>Type:</strong> ${probe.type || 'Unknown'}</div>
                <div><strong>Interval:</strong> ${probe.interval || 'Default'}</div>
                <div><strong>Enabled:</strong> ${probe.enabled !== false ? 'Yes' : 'No'}</div>
                ${probe.params ? `<div><strong>Parameters:</strong> <pre>${JSON.stringify(probe.params, null, 2)}</pre></div>` : ''}
            </div>
        </div>
    `).join('');
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
            'system-version', 'system-go-version', 'system-os', 
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
    messageDiv.innerHTML = `
        <strong>${type === 'success' ? '✅ Success:' : '❌ Error:'}</strong> ${message}
    `;
    
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