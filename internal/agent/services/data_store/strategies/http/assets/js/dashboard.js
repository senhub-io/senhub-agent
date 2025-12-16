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

        // License
        this.licenseStatusIndicator = this.base.$('#license-status-indicator');
        this.licenseStatus = this.base.$('#license-status');
        this.licenseTier = this.base.$('#license-tier');
        this.licenseExpires = this.base.$('#license-expires');
        this.licenseExpiresRow = this.base.$('#license-expires-row');
        this.licenseDays = this.base.$('#license-days');
        this.licenseDaysRow = this.base.$('#license-days-row');
        this.licenseProbesList = this.base.$('#license-probes-list');

        // Probes
        this.probesCount = this.base.$('#probes-count');
        this.probesList = this.base.$('#probes-list');
        this.noProbesDiv = this.base.$('#no-probes');
    }

    async loadDashboard() {
        try {
            this.showLoading();

            // Load system info, probes data, and license status
            const [systemData, probesData, licenseData] = await Promise.all([
                this.base.fetchAPI('info/system'),
                this.base.fetchAPI('info/probes'),
                this.base.fetchAPI('license/status')
            ]);

            this.updateAgentStatus(systemData);
            this.updateHealthStatus(systemData.health);
            this.updateResources(systemData);
            this.updateLicenseStatus(licenseData);
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

    updateLicenseStatus(licenseData) {
        if (!licenseData) {
            this.licenseStatus.textContent = 'Error';
            this.licenseTier.textContent = 'Unknown';
            this.licenseProbesList.innerHTML = '<span style="color: var(--gray-500);">-</span>';
            this.licenseStatusIndicator.className = 'status-indicator status-warning';
            return;
        }

        // Update status text and indicator
        const status = licenseData.status || 'none';
        const tier = licenseData.tier || 'free';

        // Set status text with appropriate formatting
        let statusText = status.charAt(0).toUpperCase() + status.slice(1).replace('_', ' ');
        this.licenseStatus.textContent = statusText;

        // Update tier
        this.licenseTier.textContent = tier.charAt(0).toUpperCase() + tier.slice(1);

        // Update status indicator color
        this.licenseStatusIndicator.className = 'status-indicator';
        if (status === 'active') {
            this.licenseStatusIndicator.classList.add('status-running');
        } else if (status === 'grace_period') {
            this.licenseStatusIndicator.classList.add('status-warning');
        } else if (status === 'none') {
            this.licenseStatusIndicator.classList.add('status-info');
        } else {
            this.licenseStatusIndicator.classList.add('status-warning');
        }

        // Update expiration date and days remaining
        if (licenseData.expires_at) {
            const expiresDate = new Date(licenseData.expires_at);
            this.licenseExpires.textContent = expiresDate.toLocaleDateString();
            this.licenseExpiresRow.style.display = 'flex';
        } else {
            this.licenseExpiresRow.style.display = 'none';
        }

        if (licenseData.days_remaining !== undefined) {
            this.licenseDays.textContent = licenseData.days_remaining.toString();
            this.licenseDaysRow.style.display = 'flex';
        } else {
            this.licenseDaysRow.style.display = 'none';
        }

        // Update authorized probes list
        this.updateProbesBadges(licenseData);
    }

    updateProbesBadges(licenseData) {
        const authorizedProbes = licenseData.authorized_probes || [];
        const freeTierProbes = licenseData.free_tier_probes || [];

        // Clear existing badges
        this.licenseProbesList.innerHTML = '';

        if (authorizedProbes.length > 0) {
            // Check for wildcard (Enterprise tier)
            if (authorizedProbes.includes('*')) {
                const badge = document.createElement('span');
                badge.className = 'probe-badge wildcard';
                badge.textContent = '⭐ All Probes (Enterprise)';
                this.licenseProbesList.appendChild(badge);
            } else {
                // Pro tier - show specific probes
                const sortedProbes = [...authorizedProbes].sort();
                sortedProbes.forEach(probe => {
                    const badge = document.createElement('span');
                    badge.className = 'probe-badge';
                    badge.textContent = probe;
                    this.licenseProbesList.appendChild(badge);
                });
            }
        } else {
            // Free tier only - show free tier probes
            const sortedFreeProbes = [...freeTierProbes].sort();
            sortedFreeProbes.forEach(probe => {
                const badge = document.createElement('span');
                badge.className = 'probe-badge free';
                badge.textContent = probe;
                this.licenseProbesList.appendChild(badge);
            });
        }
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