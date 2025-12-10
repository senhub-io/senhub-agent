// SenHub Agent - Documentation JavaScript

/**
 * Documentation main functionality
 */
class DocsPage {
    constructor(agentKey) {
        this.base = new SenHubBase(agentKey);
        
        this.initializeElements();
        this.loadDocumentation();
    }

    initializeElements() {
        // Status elements
        this.loadingDiv = this.base.$('#loading');
        this.errorDiv = this.base.$('#error');
        this.contentDiv = this.base.$('#content');
        
        // Stats elements
        this.totalEndpoints = this.base.$('#total-endpoints');
        this.activeProbes = this.base.$('#active-probes');
        this.enabledFormats = this.base.$('#enabled-formats');
        this.cachedMetrics = this.base.$('#cached-metrics');
        
        // Content container
        this.apiSections = this.base.$('#api-sections');
    }

    async loadDocumentation() {
        try {
            this.showLoading();
            
            // Load all required data
            const [endpointsData, probesData, systemData, formatsData] = await Promise.all([
                this.base.fetchAPI('endpoints'),
                this.base.fetchAPI('info/probes'),
                this.base.fetchAPI('info/system'),
                this.base.fetchAPI('info/endpoints')
            ]);
            
            this.updateStatistics(endpointsData, probesData, systemData, formatsData);
            this.renderEndpointsByCategory(endpointsData.endpoints || []);
            
            this.showContent();
            
        } catch (error) {
            console.error('Failed to load documentation:', error);
            this.showError();
        }
    }

    updateStatistics(endpointsData, probesData, systemData, formatsData) {
        // Update stats
        this.totalEndpoints.textContent = (endpointsData.endpoints || []).length;
        this.activeProbes.textContent = (probesData.probes || []).length;
        this.enabledFormats.textContent = (formatsData.endpoints || []).filter(e => e.enabled).length;
        this.cachedMetrics.textContent = systemData.cache?.total_metrics || 0;
    }

    renderEndpointsByCategory(endpoints) {
        // Filter: only keep GET endpoints and fix admin/debug paths
        const filteredEndpoints = endpoints.filter(endpoint => {
            // Only show GET endpoints
            return endpoint.methods && endpoint.methods.includes('GET');
        }).map(endpoint => {
            // Fix paths: admin/* should be debug/* (legacy endpoints)
            const fixedPath = endpoint.path.replace('/admin/', '/debug/');
            return { ...endpoint, path: fixedPath };
        });

        // Group endpoints by category
        const categories = {};
        filteredEndpoints.forEach(endpoint => {
            const category = endpoint.category || 'other';
            if (!categories[category]) {
                categories[category] = [];
            }
            categories[category].push(endpoint);
        });
        
        // Clear container
        this.apiSections.innerHTML = '';
        
        // Define category order and metadata (only showing main categories, others accessible via Explorer)
        const categoryInfo = {
            health: { title: '💊 Health & Status', description: 'Monitor agent health and status' },
            discovery: { title: '🔍 Discovery', description: 'Discover available probes, metrics, and schemas' },
            admin: { title: '⚙️ Administration', description: 'Manage agent configuration, logs, and cache' }
        };
        
        // Render each category
        Object.keys(categoryInfo).forEach(categoryKey => {
            if (categories[categoryKey]) {
                this.renderCategory(categoryKey, categoryInfo[categoryKey], categories[categoryKey]);
            }
        });
    }

    renderCategory(categoryKey, categoryInfo, endpoints) {
        const section = document.createElement('div');
        section.className = 'api-section';
        
        section.innerHTML = `
            <div class="card">
                <h2>${categoryInfo.title}</h2>
                <p style="color: var(--gray-600); margin-bottom: 1rem;">${categoryInfo.description}</p>
                <div class="endpoint-grid" id="category-${categoryKey}">
                    <!-- Endpoints will be added here -->
                </div>
            </div>
        `;
        
        this.apiSections.appendChild(section);
        
        const grid = section.querySelector(`#category-${categoryKey}`);
        
        endpoints.forEach(endpoint => {
            const endpointDiv = this.createEndpointItem(endpoint, categoryKey);
            grid.appendChild(endpointDiv);
        });
    }

    createEndpointItem(endpoint, categoryKey) {
        const div = document.createElement('div');
        div.className = 'endpoint-item';
        
        // Create methods badges
        const methodBadges = endpoint.methods.map(method => 
            `<span class="method-badge ${method.toLowerCase()}">${method}</span>`
        ).join('');
        
        // Create clickable URL
        const fullPath = endpoint.path.replace('{agentkey}', this.base.agentKey);
        const isExternal = endpoint.category === 'health' || endpoint.path.startsWith('/health');
        const target = isExternal ? 'target="_blank"' : '';
        
        div.innerHTML = `
            <div class="endpoint-header">
                <a href="${fullPath}" class="endpoint-path" ${target}>${endpoint.path}</a>
                <div class="endpoint-methods">${methodBadges}</div>
            </div>
            <div class="endpoint-description">${endpoint.description}</div>
            <div style="display: flex; justify-content: space-between; align-items: center; margin-top: 0.5rem;">
                <span class="endpoint-category category-${categoryKey}">${categoryKey}</span>
                <button class="btn btn-sm btn-secondary" onclick="docs.testEndpoint('${fullPath}')">
                    🧪 Test
                </button>
            </div>
        `;
        
        return div;
    }

    testEndpoint(path) {
        // Replace {agentkey} placeholder with actual agent key
        const url = path.replace('{agentkey}', this.base.agentKey);
        // Open the endpoint directly in a new tab
        window.open(url, '_blank');
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
}

// Global instance for button callbacks
let docs;

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    // Get agent key from template
    const agentKey = window.AGENT_KEY || document.querySelector('meta[name="agent-key"]')?.content;
    
    if (agentKey) {
        docs = new DocsPage(agentKey);
    } else {
        console.error('Agent key not found');
        document.getElementById('loading').style.display = 'none';
        document.getElementById('error').style.display = 'block';
    }
});