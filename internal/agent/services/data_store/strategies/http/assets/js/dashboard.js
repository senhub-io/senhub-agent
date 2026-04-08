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
            if (this.licenseProbesList) {
                this.licenseProbesList.innerHTML = '<span style="color: var(--gray-500);">-</span>';
            }
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
        if (!this.licenseProbesList) {
            console.error('License probes list element not found');
            return;
        }
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

        this.probesCount.textContent = probes.length.toString();
        this.probesList.textContent = '';

        if (probes.length === 0) {
            this.probesList.style.display = 'none';
            this.noProbesDiv.style.display = 'block';

            const uptimeText = this.agentUptime.textContent;
            const isStartingUp = this.isAgentStartingUp(uptimeText);

            const notice = document.createElement('div');
            notice.className = 'info-notice';
            notice.style.textAlign = 'center';

            const icon = document.createElement('div');
            icon.style.fontSize = '24px';
            icon.style.marginBottom = '10px';
            icon.textContent = isStartingUp ? '🔄' : '⚠️';
            notice.appendChild(icon);

            const title = document.createElement('strong');
            title.textContent = isStartingUp ? 'Probes starting up...' : 'No active probes detected';
            notice.appendChild(title);

            const desc = document.createElement('p');
            desc.style.margin = '10px 0 0 0';
            desc.style.color = '#666';
            desc.textContent = isStartingUp
                ? 'The agent just started. Probes are initializing and will appear here shortly.'
                : 'No probes are configured or all probes failed to start. Check your configuration file.';
            notice.appendChild(desc);

            this.noProbesDiv.textContent = '';
            this.noProbesDiv.appendChild(notice);
            return;
        }

        this.probesList.style.display = 'block';
        this.noProbesDiv.style.display = 'none';

        const sortedProbes = [...probes].sort();

        sortedProbes.forEach(probeName => {
            const li = document.createElement('li');
            li.className = 'probe-item';

            const metricsCount = probeMetrics[probeName] || 0;
            const isActive = metricsCount > 0;

            // Probe info row
            const infoDiv = document.createElement('div');
            infoDiv.className = 'probe-info';

            const statusDot = document.createElement('div');
            statusDot.className = `probe-status ${isActive ? 'active' : 'inactive'}`;
            infoDiv.appendChild(statusDot);

            const nameSpan = document.createElement('span');
            nameSpan.className = 'probe-name';
            nameSpan.textContent = probeName;
            infoDiv.appendChild(nameSpan);

            li.appendChild(infoDiv);

            // Metrics count
            const metricsDiv = document.createElement('div');
            metricsDiv.className = 'probe-metrics';
            metricsDiv.textContent = `${metricsCount} metrics`;
            li.appendChild(metricsDiv);

            // Key values container
            const valuesDiv = document.createElement('div');
            valuesDiv.className = 'probe-key-values';
            valuesDiv.id = `probe-values-${probeName}`;
            li.appendChild(valuesDiv);

            this.probesList.appendChild(li);

            if (isActive) {
                this.loadProbeKeyValues(probeName);
            }
        });
    }

    async loadProbeKeyValues(probeName) {
        try {
            const data = await this.base.fetchAPI(`prtg/metrics/${probeName}`);
            const results = data?.prtg?.result || [];
            if (results.length === 0) return;

            const container = this.base.$(`#probe-values-${probeName}`);
            if (!container) return;

            const keyMetrics = this.selectKeyMetrics(results, probeName);

            keyMetrics.forEach(m => {
                const span = document.createElement('span');
                span.className = 'key-metric';
                span.title = m.channel;

                const valueEl = document.createElement('strong');
                valueEl.textContent = typeof m.value === 'number'
                    ? (Number.isInteger(m.value) ? m.value : m.value.toFixed(1))
                    : m.value;
                span.appendChild(valueEl);

                const unit = m.customunit || m.unit || '';
                if (unit && unit !== '#') {
                    span.appendChild(document.createTextNode(' ' + unit));
                }

                const label = document.createElement('small');
                label.textContent = ' ' + this.shortenLabel(m.channel);
                span.appendChild(label);

                container.appendChild(span);
            });
        } catch (e) {
            // Silently fail — key values are optional enhancement
        }
    }

    selectKeyMetrics(results, probeName) {
        const priorities = {
            cpu: ['CPU Total Usage', 'CPU Load Average 1min'],
            memory: ['Memory Usage', 'Memory Used', 'Memory Free'],
            logicaldisk: ['Used Percent', 'Free Bytes', 'Available Bytes'],
            citrix: ['Sessions Connected', 'Sessions Disconnected', 'Logon Duration Total', 'Machines Total'],
            netscaler: ['System CPU Usage', 'System Memory Usage', 'System HTTP Requests Rate'],
        };

        const probeType = probeName.toLowerCase();
        const priorityList = priorities[probeType] || [];

        const selected = [];
        for (const prio of priorityList) {
            const match = results.find(r => r.channel && r.channel.includes(prio));
            if (match && selected.length < 4) selected.push(match);
        }

        if (selected.length < 4) {
            for (const r of results) {
                if (selected.length >= 4) break;
                if (!selected.includes(r) && r.value !== 0) selected.push(r);
            }
        }

        return selected.slice(0, 4);
    }

    shortenLabel(channel) {
        return channel
            .replace(/^(CPU |Memory |System |Sessions |Logon |Machines )/, '')
            .replace(/\s*\(.*\)$/, '');
    }

    /**
     * Check if agent is starting up (uptime < 2 minutes)
     * @param {string} uptimeText - Uptime string (e.g., "1m 30s", "2h 15m")
     * @returns {boolean} - True if uptime < 2 minutes
     */
    isAgentStartingUp(uptimeText) {
        if (!uptimeText || uptimeText === 'unknown') {
            return true; // Assume starting if uptime unknown
        }

        // Parse uptime string: "1h 2m 3s", "1m 30s", "45s", etc.
        const parts = uptimeText.split(' ');
        let totalSeconds = 0;

        for (const part of parts) {
            if (part.includes('h')) {
                totalSeconds += parseInt(part) * 3600;
            } else if (part.includes('m')) {
                totalSeconds += parseInt(part) * 60;
            } else if (part.includes('s')) {
                totalSeconds += parseInt(part);
            } else if (part.includes('d')) {
                totalSeconds += parseInt(part) * 86400;
            }
        }

        // Starting up if uptime < 2 minutes (120 seconds)
        return totalSeconds < 120;
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