// SenHub Agent - Sensor Builder JavaScript

/**
 * Sensor Builder main functionality
 */
class APIExplorer {
    constructor(agentKey) {
        this.base = new SenHubBase(agentKey);
        this.selectedProbe = null;
        this.selectedTags = {};
        this.isEditMode = false;
        this.tableHTML = null;
        this.jsonText = null;
        this.currentView = 'table';
        this.autoFetchTimeout = null;
        this.initialized = false;

        this.initializeElements();
        this.setupEventListeners();
        this.loadInitialData();
    }

    initializeElements() {
        // Form elements
        this.endpointTypeSelect = this.base.$('#endpoint-type');
        this.probeSelect = this.base.$('#probe-select');
        this.showTagsGroup = this.base.$('#show-tags-group');
        this.tagFiltersGroup = this.base.$('#tag-filters-group');
        this.tagFiltersContainer = this.base.$('#tag-filters');
        this.showTagsCheckbox = this.base.$('#show-tags-checkbox');


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

        // View toggle
        this.responseViewToggle = this.base.$('#response-view-toggle');
        this.viewTableBtn = this.base.$('#view-table-btn');
        this.viewJsonBtn = this.base.$('#view-json-btn');

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

        // Show tags checkbox
        if (this.showTagsCheckbox) {
            this.showTagsCheckbox.addEventListener('change', () => {
                this.generateURL();
            });
        } else {
        }

        // Button clicks
        this.editUrlBtn?.addEventListener('click', () => this.toggleEditMode());
        this.copyUrlBtn?.addEventListener('click', () => this.copyURL());
        this.testRequestBtn?.addEventListener('click', () => this.testRequest());
        this.copyResponseBtn?.addEventListener('click', () => this.copyResponse());
        this.clearResponseBtn?.addEventListener('click', () => this.clearResponse());

        // View toggle buttons
        this.viewTableBtn?.addEventListener('click', () => this.switchView('table'));
        this.viewJsonBtn?.addEventListener('click', () => this.switchView('json'));

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
        
        // Restore state from URL after data is loaded
        const urlState = this.restoreStateFromURL();
        
        // If we have a probe from URL, load its tags and update UI
        if (urlState.probe) {
            await this.tagFilters.loadTags(urlState.probe);
            this.showTagFilters(true);
            
            // Restore tag filter selections
            this.restoreTagFilterSelections(urlState.tags);
        }
        
        // Generate URL with restored state
        this.generateURL();

        // Mark as initialized — enable auto-fetch for subsequent changes
        this.initialized = true;

        // Initial fetch if we have both endpoint and probe from URL
        if (urlState.endpointType && urlState.probe) {
            this.testRequest();
        }
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
        if (this.showTagsGroup) {
            this.showTagsGroup.style.display = show ? 'block' : 'none';
        }
        if (this.tagFiltersGroup) {
            this.tagFiltersGroup.style.display = show ? 'block' : 'none';
        }
    }

    generateURL() {
        const endpointType = this.endpointTypeSelect?.value;
        const isPrometheus = endpointType === 'prometheus';

        // Prometheus exposes the whole agent at a single endpoint, so the
        // probe selector is not required for it. Other formats route per
        // probe and still need a probe selection.
        if (!endpointType || (!this.selectedProbe && !isPrometheus)) {
            this.updateUrlDisplay(
                isPrometheus
                    ? 'Select an endpoint to generate URL...'
                    : 'Select an endpoint and probe to generate URL...'
            );
            this.setButtonsEnabled(false);
            this.updatePageURL();
            return;
        }

        let url;
        if (isPrometheus) {
            url = `/api/${this.base.agentKey}/prometheus/metrics`;
        } else {
            url = `/api/${this.base.agentKey}/${endpointType}/metrics/${this.selectedProbe}`;
        }

        // Add tag filters as query parameters
        const queryParams = [];
        Object.entries(this.selectedTags).forEach(([key, value]) => {
            queryParams.push(`tags=${encodeURIComponent(key)}:${encodeURIComponent(value)}`);
        });

        // Add show_tags parameter if checkbox is unchecked
        if (this.showTagsCheckbox) {
            if (!this.showTagsCheckbox.checked) {
                queryParams.push('show_tags=false');
            }
        } else {
        }

        if (queryParams.length > 0) {
            url += '?' + queryParams.join('&');
        }

        const fullUrl = window.location.origin + url;
        this.updateUrlDisplay(fullUrl);
        this.setButtonsEnabled(true);

        // Update the page URL to reflect current state
        this.updatePageURL(endpointType, this.selectedProbe, this.selectedTags);

        // Auto-fetch as soon as the URL is buildable. For Prometheus the
        // probe selection is optional; for the others a probe is required.
        if (endpointType && (this.selectedProbe || isPrometheus) && this.initialized) {
            if (this.autoFetchTimeout) {
                clearTimeout(this.autoFetchTimeout);
            }
            this.autoFetchTimeout = setTimeout(() => this.testRequest(), 300);
        }
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
            this.editUrlBtn.innerHTML = 'Lock URL';
        } else {
            // Switch back to generated mode
            this.manualUrlInput.style.display = 'none';
            this.generatedUrlDiv.style.display = 'block';
            this.editUrlBtn.innerHTML = 'Edit URL';
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
            this.showButtonSuccess(this.copyUrlBtn, 'Copied!', 'Copy Sensor URL');
        }
    }

    async testRequest() {
        const url = this.getCurrentURL().replace(window.location.origin, '');
        const endpointType = this.endpointTypeSelect?.value;

        this.responseArea.className = 'result-area centered';
        this.setLoadingState();
        this.copyResponseBtn.disabled = true;
        this.tableHTML = null;
        this.jsonText = null;

        try {
            const response = await fetch(url);

            // Text-only formats (Nagios performance line, Prometheus
            // exposition) must NOT go through JSON.parse — they're plain
            // text and the parser fails on the very first token.
            if (endpointType === 'nagios' || endpointType === 'prometheus') {
                const responseText = await response.text();
                this.jsonText = responseText;
                this.tableHTML = null;

                if (this.responseViewToggle) {
                    this.responseViewToggle.style.display = 'none';
                }

                this.responseArea.className = 'result-area';
                this.responseArea.textContent = responseText;
            } else {
                const jsonData = await response.json();
                this.jsonText = JSON.stringify(jsonData, null, 2);

                // Try to render table for PRTG/API format
                if (jsonData.prtg?.result) {
                    this.tableHTML = this.buildResponseTable(jsonData);

                    // Show view toggle
                    if (this.responseViewToggle) {
                        this.responseViewToggle.style.display = 'flex';
                    }

                    // Show table view by default
                    this.currentView = 'table';
                    this.viewTableBtn?.classList.add('active');
                    this.viewJsonBtn?.classList.remove('active');
                    this.showTableView();
                } else {
                    this.tableHTML = null;

                    // Hide view toggle for non-PRTG JSON
                    if (this.responseViewToggle) {
                        this.responseViewToggle.style.display = 'none';
                    }

                    this.responseArea.className = 'result-area';
                    this.responseArea.textContent = this.jsonText;
                }
            }

            this.copyResponseBtn.disabled = false;

        } catch (error) {
            this.responseArea.className = 'result-area centered';
            this.responseArea.textContent = `Error: ${error.message}`;
            this.copyResponseBtn.disabled = true;

            if (this.responseViewToggle) {
                this.responseViewToggle.style.display = 'none';
            }
        }
    }

    setLoadingState() {
        this.responseArea.textContent = '';
        const loadingSpan = document.createElement('span');
        loadingSpan.className = 'loading-text';
        loadingSpan.textContent = 'Loading...';
        this.responseArea.appendChild(loadingSpan);
    }

    buildResponseTable(prtgData) {
        const results = prtgData.prtg?.result || [];
        if (results.length === 0) return null;

        // Build table using DOM methods for safety, then serialize
        const table = document.createElement('table');
        table.className = 'metrics-table';

        const thead = document.createElement('thead');
        const headerRow = document.createElement('tr');
        ['Metric', 'Value', 'Unit'].forEach(text => {
            const th = document.createElement('th');
            th.textContent = text;
            headerRow.appendChild(th);
        });
        thead.appendChild(headerRow);
        table.appendChild(thead);

        const tbody = document.createElement('tbody');
        results.forEach(r => {
            const tr = document.createElement('tr');

            const tdChannel = document.createElement('td');
            tdChannel.textContent = r.channel || '';
            tr.appendChild(tdChannel);

            const tdValue = document.createElement('td');
            tdValue.className = 'value-cell';
            const value = typeof r.value === 'number' ?
                (Number.isInteger(r.value) ? r.value : r.value.toFixed(2)) : r.value;
            tdValue.textContent = value;
            tr.appendChild(tdValue);

            const tdUnit = document.createElement('td');
            tdUnit.textContent = r.customunit || r.unit || '';
            tr.appendChild(tdUnit);

            tbody.appendChild(tr);
        });
        table.appendChild(tbody);

        // Wrap in a container div
        const container = document.createElement('div');
        container.appendChild(table);
        return container;
    }

    showTableView() {
        this.responseArea.className = 'result-area';
        // Clear and append the table DOM element
        this.responseArea.textContent = '';
        if (this.tableHTML) {
            this.responseArea.appendChild(this.tableHTML.cloneNode(true));
        } else {
            const p = document.createElement('p');
            p.textContent = 'No metrics found';
            this.responseArea.appendChild(p);
        }
    }

    switchView(view) {
        this.currentView = view;

        if (view === 'table') {
            this.viewTableBtn?.classList.add('active');
            this.viewJsonBtn?.classList.remove('active');
            if (this.tableHTML) {
                this.showTableView();
            }
        } else {
            this.viewJsonBtn?.classList.add('active');
            this.viewTableBtn?.classList.remove('active');
            if (this.jsonText) {
                this.responseArea.className = 'result-area';
                this.responseArea.textContent = this.jsonText;
            }
        }
    }

    async copyResponse() {
        const success = await this.base.copyToClipboard(this.responseArea.textContent);
        
        if (success) {
            this.showButtonSuccess(this.copyResponseBtn, 'Copied!', 'Copy Response');
        }
    }

    clearResponse() {
        this.responseArea.className = 'result-area centered';
        this.responseArea.textContent = '';
        const placeholder = document.createElement('span');
        placeholder.className = 'placeholder-text';
        placeholder.textContent = 'Click "Preview" to see the response...';
        this.responseArea.appendChild(placeholder);
        this.copyResponseBtn.disabled = true;
        this.tableHTML = null;
        this.jsonText = null;

        // Hide view toggle
        if (this.responseViewToggle) {
            this.responseViewToggle.style.display = 'none';
        }
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

    updatePageURL(endpointType = '', probe = '', tags = {}) {
        const params = new URLSearchParams();
        
        if (endpointType) params.set('endpoint', endpointType);
        if (probe) params.set('probe', probe);
        
        // Add tags as individual parameters
        Object.entries(tags).forEach(([key, value]) => {
            params.set(`tag_${key}`, value);
        });
        
        const newURL = params.toString() ? 
            `${window.location.pathname}?${params.toString()}` : 
            window.location.pathname;
            
        // Update URL without triggering page reload
        window.history.replaceState({}, '', newURL);
    }

    restoreStateFromURL() {
        const urlParams = new URLSearchParams(window.location.search);
        
        // Restore endpoint type
        const endpointType = urlParams.get('endpoint');
        if (endpointType && this.endpointTypeSelect) {
            this.endpointTypeSelect.value = endpointType;
        }
        
        // Restore probe selection
        const probe = urlParams.get('probe');
        if (probe && this.probeSelect) {
            this.probeSelect.value = probe;
            this.selectedProbe = probe;
        }
        
        // Restore tag filters
        const tags = {};
        for (const [key, value] of urlParams.entries()) {
            if (key.startsWith('tag_')) {
                const tagKey = key.substring(4); // Remove 'tag_' prefix
                tags[tagKey] = value;
            }
        }
        this.selectedTags = tags;
        
        return { endpointType, probe, tags };
    }

    restoreTagFilterSelections(tags) {
        // This method would need to interact with the TagFilters instance
        // to programmatically select the appropriate tag values
        Object.entries(tags).forEach(([tagKey, tagValue]) => {
            // Enable the tag checkbox
            const tagCheckbox = this.base.$(`#tag-${tagKey}`);
            if (tagCheckbox) {
                tagCheckbox.checked = true;
                
                // If it's a comma-separated list, select individual values
                const values = tagValue.split(',');
                values.forEach(value => {
                    const valueCheckbox = this.base.$(`#value-${tagKey}-${this.tagFilters.escapeId(value)}`);
                    if (valueCheckbox) {
                        valueCheckbox.checked = true;
                    }
                });
                
                // Update UI to show the values container
                this.tagFilters.updateTagUI(tagKey, 'multi', true);
            }
        });
        
        // Trigger the tag update to reflect restored selections
        this.tagFilters.updateSelectedTags();
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