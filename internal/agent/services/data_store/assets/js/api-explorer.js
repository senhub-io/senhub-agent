// SenHub Agent - API Explorer JavaScript

/**
 * API Explorer main functionality
 */
class APIExplorer {
    constructor(agentKey) {
        this.base = new SenHubBase(agentKey);
        this.selectedProbe = null;
        this.selectedTags = {};
        this.isEditMode = false;
        
        this.initializeElements();
        this.setupEventListeners();
        this.loadInitialData();
    }

    initializeElements() {
        // Form elements
        this.endpointTypeSelect = this.base.$('#endpoint-type');
        this.probeSelect = this.base.$('#probe-select');
        this.tagFiltersGroup = this.base.$('#tag-filters-group');
        this.tagFiltersContainer = this.base.$('#tag-filters');
        
        // URL elements
        this.generatedUrlDiv = this.base.$('#generated-url');
        this.manualUrlInput = this.base.$('#manual-url');
        
        // Buttons
        this.editUrlBtn = this.base.$('#edit-url-btn');
        this.copyUrlBtn = this.base.$('#copy-url-btn');
        this.testRequestBtn = this.base.$('#test-request-btn');
        this.copyResponseBtn = this.base.$('#copy-response-btn');
        this.clearResponseBtn = this.base.$('#clear-response-btn');
        
        // Response area
        this.responseArea = this.base.$('#response-area');

        // Initialize tag filters
        this.tagFilters = new TagFilters(
            this.base, 
            this.tagFiltersContainer, 
            (tags) => this.onTagsChanged(tags)
        );
    }

    setupEventListeners() {
        // Form changes
        this.endpointTypeSelect?.addEventListener('change', () => this.generateURL());
        this.probeSelect?.addEventListener('change', (e) => this.onProbeChange(e.target.value));
        
        // Button clicks
        this.editUrlBtn?.addEventListener('click', () => this.toggleEditMode());
        this.copyUrlBtn?.addEventListener('click', () => this.copyURL());
        this.testRequestBtn?.addEventListener('click', () => this.testRequest());
        this.copyResponseBtn?.addEventListener('click', () => this.copyResponse());
        this.clearResponseBtn?.addEventListener('click', () => this.clearResponse());
        
        // Manual URL input
        this.manualUrlInput?.addEventListener('input', 
            this.base.debounce(() => this.onManualUrlChange(), 300)
        );
    }

    async loadInitialData() {
        await Promise.all([
            this.loadEndpoints(),
            this.loadProbes()
        ]);
    }

    async loadEndpoints() {
        try {
            const data = await this.base.fetchAPI('info/endpoints');
            const endpoints = data.endpoints || [];
            
            this.endpointTypeSelect.innerHTML = '<option value="">Select an endpoint...</option>';
            endpoints.forEach(endpoint => {
                const option = document.createElement('option');
                option.value = endpoint.name;
                option.textContent = `${endpoint.name.toUpperCase()} - ${endpoint.description}`;
                this.endpointTypeSelect.appendChild(option);
            });
            
        } catch (error) {
            this.endpointTypeSelect.innerHTML = '<option value="">Error loading endpoints</option>';
            console.error('Failed to load endpoints:', error);
        }
    }

    async loadProbes() {
        try {
            const data = await this.base.fetchAPI('info/probes');
            const probes = data.probes || [];
            
            this.probeSelect.innerHTML = '<option value="">Select a probe...</option>';
            probes.sort().forEach(probe => {
                const option = document.createElement('option');
                option.value = probe;
                option.textContent = probe;
                this.probeSelect.appendChild(option);
            });
            
        } catch (error) {
            this.probeSelect.innerHTML = '<option value="">Error loading probes</option>';
            console.error('Failed to load probes:', error);
        }
    }

    async onProbeChange(probeName) {
        this.selectedProbe = probeName;
        
        if (probeName) {
            await this.tagFilters.loadTags(probeName);
            this.showTagFilters(true);
        } else {
            this.tagFilters.clearTags();
            this.showTagFilters(false);
        }
        
        this.generateURL();
    }

    onTagsChanged(tags) {
        this.selectedTags = tags;
        this.generateURL();
    }

    showTagFilters(show) {
        if (this.tagFiltersGroup) {
            this.tagFiltersGroup.style.display = show ? 'block' : 'none';
        }
    }

    generateURL() {
        const endpointType = this.endpointTypeSelect?.value;
        
        if (!endpointType || !this.selectedProbe) {
            this.updateUrlDisplay('Select an endpoint and probe to generate URL...');
            this.setButtonsEnabled(false);
            return;
        }

        let url = `/api/${this.base.agentKey}/${endpointType}/metrics/${this.selectedProbe}`;
        
        // Add tag filters as query parameters
        const tagParams = [];
        Object.entries(this.selectedTags).forEach(([key, value]) => {
            tagParams.push(`tags=${key}:${value}`);
        });
        
        if (tagParams.length > 0) {
            url += '?' + tagParams.join('&');
        }
        
        const fullUrl = window.location.origin + url;
        this.updateUrlDisplay(fullUrl);
        this.setButtonsEnabled(true);
    }

    updateUrlDisplay(url) {
        if (this.generatedUrlDiv) {
            this.generatedUrlDiv.textContent = url;
        }
        
        // Update manual input if in edit mode
        if (this.isEditMode && this.manualUrlInput) {
            this.manualUrlInput.value = url;
        }
    }

    setButtonsEnabled(enabled) {
        const buttons = [this.editUrlBtn, this.copyUrlBtn, this.testRequestBtn];
        buttons.forEach(btn => {
            if (btn) {
                btn.disabled = !enabled;
            }
        });
    }

    toggleEditMode() {
        this.isEditMode = !this.isEditMode;
        
        if (this.isEditMode) {
            // Switch to edit mode
            this.manualUrlInput.style.display = 'block';
            this.generatedUrlDiv.style.display = 'none';
            this.manualUrlInput.value = this.generatedUrlDiv.textContent;
            this.editUrlBtn.innerHTML = '🔒 Lock URL';
        } else {
            // Switch back to generated mode
            this.manualUrlInput.style.display = 'none';
            this.generatedUrlDiv.style.display = 'block';
            this.editUrlBtn.innerHTML = '✏️ Edit URL';
            this.generateURL(); // Regenerate URL
        }
    }

    onManualUrlChange() {
        // In edit mode, the manual input is the source of truth
        // We could add URL validation here
    }

    getCurrentURL() {
        if (this.isEditMode && this.manualUrlInput) {
            return this.manualUrlInput.value;
        } else if (this.generatedUrlDiv) {
            return this.generatedUrlDiv.textContent;
        }
        return '';
    }

    async copyURL() {
        const url = this.getCurrentURL();
        const success = await this.base.copyToClipboard(url);
        
        if (success) {
            this.showButtonSuccess(this.copyUrlBtn, '✅ Copied!', '📋 Copy URL');
        }
    }

    async testRequest() {
        const url = this.getCurrentURL().replace(window.location.origin, '');
        const endpointType = this.endpointTypeSelect?.value;
        
        this.responseArea.className = 'result-area centered';
        this.responseArea.innerHTML = '<span class="loading-text">Loading...</span>';
        this.copyResponseBtn.disabled = true;
        
        try {
            const response = await fetch(url);
            
            let responseText;
            if (endpointType === 'nagios') {
                responseText = await response.text();
            } else {
                const jsonData = await response.json();
                responseText = JSON.stringify(jsonData, null, 2);
            }
            
            // Remove centered class and set content
            this.responseArea.className = 'result-area';
            this.responseArea.textContent = responseText;
            this.copyResponseBtn.disabled = false;
            
        } catch (error) {
            this.responseArea.className = 'result-area centered';
            this.responseArea.textContent = `Error: ${error.message}`;
            this.copyResponseBtn.disabled = true;
        }
    }

    async copyResponse() {
        const success = await this.base.copyToClipboard(this.responseArea.textContent);
        
        if (success) {
            this.showButtonSuccess(this.copyResponseBtn, '✅ Copied!', '📋 Copy Response');
        }
    }

    clearResponse() {
        this.responseArea.className = 'result-area centered';
        this.responseArea.innerHTML = '<span class="placeholder-text">Click "Test Request" to see the response...</span>';
        this.copyResponseBtn.disabled = true;
    }

    showButtonSuccess(button, successText, originalText) {
        if (!button) return;
        
        const original = button.textContent;
        button.textContent = successText;
        button.disabled = true;
        
        setTimeout(() => {
            button.textContent = originalText || original;
            button.disabled = false;
        }, 2000);
    }
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    // Get agent key from template
    const agentKey = window.AGENT_KEY || document.querySelector('meta[name="agent-key"]')?.content;
    
    if (agentKey) {
        window.apiExplorer = new APIExplorer(agentKey);
    } else {
        console.error('Agent key not found');
    }
});