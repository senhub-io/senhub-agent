// SenHub Agent - Dashboard JavaScript

/**
 * Dashboard main functionality
 */
class Dashboard {
    constructor(agentKey) {
        this.base = new SenHubBase(agentKey);
        this.refreshInterval = null;
        
        this.initializeElements();
        this.loadDashboard();
        this.startAutoRefresh();
    }

    initializeElements() {
        // Status elements
        this.loadingDiv = this.base.$('#loading');
        this.errorDiv = this.base.$('#error');
        this.contentDiv = this.base.$('#content');
        
        // Agent status
        this.agentStatusIndicator = this.base.$('#agent-status-indicator');
        this.agentStatus = this.base.$('#agent-status');
        this.agentVersion = this.base.$('#agent-version');
        this.agentCommit = this.base.$('#agent-commit');
        this.agentCommitRow = this.base.$('#agent-commit-row');
        this.agentPort = this.base.$('#agent-port');
        this.agentUptime = this.base.$('#agent-uptime');
        
        // Health check
        this.healthHttp = this.base.$('#health-http');
        this.healthCache = this.base.$('#health-cache');
        this.healthMetrics = this.base.$('#health-metrics');
        this.healthTimestamp = this.base.$('#health-timestamp');
        
        // Resources
        this.resourceMemory = this.base.$('#resource-memory');
        this.resourceGoroutines = this.base.$('#resource-goroutines');
        this.resourceCacheTtl = this.base.$('#resource-cache-ttl');
        this.resourceCpuUsage = this.base.$('#resource-cpu-usage');
        
        // Probes
        this.probesCount = this.base.$('#probes-count');
        this.probesList = this.base.$('#probes-list');
        this.noProbesDiv = this.base.$('#no-probes');
    }

    async loadDashboard() {
        try {
            this.showLoading();
            
            // Load system info and probes data
            const [systemData, probesData] = await Promise.all([
                this.base.fetchAPI('info/system'),
                this.base.fetchAPI('info/probes')
            ]);
            
            this.updateAgentStatus(systemData);
            this.updateHealthStatus(systemData.health);
            this.updateResources(systemData);
            this.updateProbesList(probesData);
            
            this.showContent();
            
        } catch (error) {
            console.error('Failed to load dashboard:', error);
            this.showError();
        }
    }

    updateAgentStatus(systemData) {
        // Update agent status
        this.agentStatus.textContent = systemData.status || 'unknown';
        this.agentVersion.textContent = systemData.version || 'unknown';
        this.agentPort.textContent = systemData.port || 'unknown';
        this.agentUptime.textContent = systemData.uptime || 'unknown';
        
        // Update commit information if available
        if (systemData.commit && systemData.commit.trim() !== '') {
            this.agentCommit.textContent = systemData.commit;
            this.agentCommitRow.style.display = 'flex';
        } else {
            this.agentCommitRow.style.display = 'none';
        }
        
        // Update status indicator
        this.agentStatusIndicator.className = 'status-indicator';
        if (systemData.status === 'running') {
            this.agentStatusIndicator.classList.add('status-running');
        } else {
            this.agentStatusIndicator.classList.add('status-warning');
        }
    }

    updateHealthStatus(health) {
        if (!health) return;
        
        // Update health services
        const services = health.services || {};
        this.healthHttp.textContent = services.http_server || 'unknown';
        this.healthCache.textContent = services.cache || 'unknown';
        this.healthMetrics.textContent = services.metrics || 'unknown';
        
        // Update timestamp
        if (health.timestamp) {
            const date = new Date(health.timestamp * 1000);
            this.healthTimestamp.textContent = date.toLocaleTimeString();
        }
    }

    updateResources(systemData) {
        const resources = systemData.resources || {};
        const cache = systemData.cache || {};
        
        // Update resource metrics
        if (resources.memory_usage_mb !== undefined) {
            this.resourceMemory.textContent = `${resources.memory_usage_mb.toFixed(2)} MB`;
        }
        
        if (resources.goroutines !== undefined) {
            this.resourceGoroutines.textContent = resources.goroutines.toString();
        }
        
        if (resources.cpu_percent !== undefined) {
            this.resourceCpuUsage.textContent = `${resources.cpu_percent.toFixed(1)}%`;
        }
        
        this.resourceCacheTtl.textContent = cache.ttl || 'unknown';
    }

    updateProbesList(probesData) {
        const probes = probesData.probes || [];
        const probeMetrics = probesData.probe_metrics || {};
        
        // Update probes count
        this.probesCount.textContent = probes.length.toString();
        
        // Clear existing list
        this.probesList.innerHTML = '';
        
        if (probes.length === 0) {
            this.probesList.style.display = 'none';
            this.noProbesDiv.style.display = 'block';
            return;
        }
        
        this.probesList.style.display = 'block';
        this.noProbesDiv.style.display = 'none';
        
        // Sort probes alphabetically
        const sortedProbes = [...probes].sort();
        
        sortedProbes.forEach(probeName => {
            const li = document.createElement('li');
            li.className = 'probe-item';
            
            const metricsCount = probeMetrics[probeName] || 0;
            const isActive = metricsCount > 0;
            
            li.innerHTML = `
                <div class="probe-info">
                    <div class="probe-status ${isActive ? 'active' : 'inactive'}"></div>
                    <span class="probe-name">${probeName}</span>
                </div>
                <div class="probe-metrics">
                    ${metricsCount} metrics
                </div>
            `;
            
            this.probesList.appendChild(li);
        });
    }

    showLoading() {
        this.loadingDiv.style.display = 'block';
        this.errorDiv.style.display = 'none';
        this.contentDiv.style.display = 'none';
    }

    showContent() {
        this.loadingDiv.style.display = 'none';
        this.errorDiv.style.display = 'none';
        this.contentDiv.style.display = 'block';
    }

    showError() {
        this.loadingDiv.style.display = 'none';
        this.errorDiv.style.display = 'block';
        this.contentDiv.style.display = 'none';
    }

    startAutoRefresh() {
        // Auto-refresh every 30 seconds
        this.refreshInterval = setInterval(() => {
            this.loadDashboard();
        }, 30000);
    }

    stopAutoRefresh() {
        if (this.refreshInterval) {
            clearInterval(this.refreshInterval);
            this.refreshInterval = null;
        }
    }
}

// Global function for error retry
function loadDashboard() {
    if (window.dashboard) {
        window.dashboard.loadDashboard();
    }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    // Get agent key from template
    const agentKey = window.AGENT_KEY || document.querySelector('meta[name="agent-key"]')?.content;
    
    if (agentKey) {
        window.dashboard = new Dashboard(agentKey);
    } else {
        console.error('Agent key not found');
        document.getElementById('loading').style.display = 'none';
        document.getElementById('error').style.display = 'block';
    }
});

// Cleanup on page unload
window.addEventListener('beforeunload', () => {
    if (window.dashboard) {
        window.dashboard.stopAutoRefresh();
    }
});