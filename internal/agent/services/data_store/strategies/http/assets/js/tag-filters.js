// SenHub Agent - Tag Filters Module

/**
 * Advanced tag filtering with multi-selection support
 */
class TagFilters {
    constructor(base, container, onTagsChanged) {
        this.base = base;
        this.container = container;
        this.onTagsChanged = onTagsChanged;
        this.selectedTags = {};
        this.availableTags = {};
        
        this.setupEventListeners();
    }

    setupEventListeners() {
        // Use event delegation for dynamic content
        this.container.addEventListener('change', (e) => {
            if (e.target.matches('input[type="checkbox"]') || e.target.matches('select')) {
                // Auto-check parent when a value is selected
                if (e.target.matches('input[data-tag]:not([id^="tag-"])')) {
                    this.handleValueCheckboxChange(e.target);
                }
                this.updateSelectedTags();
            }
        });

        // Listen for expression input changes (text fields)
        this.container.addEventListener('input', (e) => {
            if (e.target.matches('.expression-input')) {
                // Debounce the update to avoid too many calls while typing
                clearTimeout(this.expressionTimeout);
                this.expressionTimeout = setTimeout(() => {
                    this.updateSelectedTags();
                }, 300);
            }
        });

        // Add click handlers for toggling tag sections
        this.container.addEventListener('click', (e) => {
            if (e.target.matches('.tag-header-clickable, .tag-header-clickable *')) {
                // Find the closest tag header
                const header = e.target.closest('.tag-header-clickable') || e.target;
                if (header.classList.contains('tag-header-clickable')) {
                    this.toggleTagSection(header);
                }
            }
        });
    }

    async loadTags(probeName) {
        if (!probeName) {
            this.clearTags();
            return;
        }

        try {
            this.base.showLoading(this.container, 'Loading tag filters...');
            
            const data = await this.base.fetchAPI(`info/tags/${probeName}`);
            this.availableTags = data.tags || {};
            
            this.renderTags();
            this.updateSelectedTags();
            
        } catch (error) {
            this.base.showError(this.container, `Failed to load tags: ${error.message}`);
        }
    }

    renderTags() {
        // Filter out redundant tags (technical/internal tags not useful for filtering)
        const redundantTags = [
            'host', 'probe_name', 'platform', 'os', 'prtg_metric_id',
            'drive_id', 'volume_id', 'pool_id', 'adapter', 'connection_name',
            'ha_node_ip', 'is_local_node', 'vserver_ip', 'vserver_type'  // HA/VServer technical tags
        ];
        const alwaysKeepTags = ['url', 'endpoint', 'interface', 'drive', 'drive_name', 'volume_name', 'volume_type', 'pool_name', 'controller', 'raid_type', 'core', 'metric_view', 'metric_type'];
// Note: fan_name and sensor_name removed - thermal metrics disabled for consistency
// Note: metric_view and metric_type always shown - functional grouping tags for filtering
// Note: ha_node_ip, is_local_node, vserver_ip, vserver_type hidden - too technical for UI filtering

        const filteredTags = Object.fromEntries(
            Object.entries(this.availableTags)
                .filter(([tagKey, tagInfo]) => {
                    if (redundantTags.includes(tagKey)) return false;
                    if (alwaysKeepTags.includes(tagKey)) return true;

                    const values = tagInfo.values || [];
                    return values.length > 1; // Only show tags with multiple values
                })
                .sort(([a], [b]) => {
                    // Always put metric_view and metric_type first
                    if (a === 'metric_view') return -1;
                    if (b === 'metric_view') return 1;
                    if (a === 'metric_type') return -1;
                    if (b === 'metric_type') return 1;
                    return a.localeCompare(b);
                })
        );

        if (Object.keys(filteredTags).length === 0) {
            this.container.innerHTML = '<p class="info-notice">No filterable tags available for this probe.</p>';
            return;
        }

        const html = `
            <div class="info-notice">
                💡 <strong>Multi-value support:</strong> Select multiple values per tag for advanced filtering
            </div>
            <div class="tags-grid">
                ${Object.entries(filteredTags).map(([tagKey, tagInfo]) => 
                    this.renderTagFilter(tagKey, tagInfo)
                ).join('')}
            </div>
        `;

        this.container.innerHTML = html;
    }

    renderTagFilter(tagKey, tagInfo) {
        const values = tagInfo.values || [];
        const description = tagInfo.description && tagInfo.description !== "No description available" 
            ? ` - ${tagInfo.description}` 
            : '';

        return `
            <div class="tag-filter-container" data-tag="${tagKey}">
                <div class="tag-header">
                    <input type="checkbox" id="tag-${tagKey}" data-tag="${tagKey}">
                    <div class="tag-header-clickable" data-tag="${tagKey}">
                        <label for="tag-${tagKey}">${tagKey} (${values.length} values)${description}</label>
                        <span class="toggle-indicator">▼</span>
                    </div>
                    <select class="mode-select" id="mode-${tagKey}" disabled>
                        <option value="multi">Multi-select</option>
                        <option value="expression">Expression</option>
                    </select>
                </div>
                
                <div class="values-container" id="values-${tagKey}" style="display: none;">
                    <div class="value-option">
                        <input type="checkbox" id="value-${tagKey}-ALL" value="ALL" data-tag="${tagKey}">
                        <label for="value-${tagKey}-ALL" class="all-values-label">All values</label>
                    </div>
                    ${values.sort().map(value => `
                        <div class="value-option">
                            <input type="checkbox" id="value-${tagKey}-${this.escapeId(value)}" 
                                   value="${this.escapeHtml(value)}" data-tag="${tagKey}">
                            <label for="value-${tagKey}-${this.escapeId(value)}">${this.escapeHtml(value)}</label>
                        </div>
                    `).join('')}
                </div>
                
                <div class="expression-container" id="expression-${tagKey}" style="display: none;">
                    <input type="text" class="expression-input" id="expression-input-${tagKey}" 
                           placeholder="e.g: value1,value2 or value* or [0-9]+" data-tag="${tagKey}">
                    <small class="expression-help">
                        Supports: comma-separated values, wildcards (*), regex patterns
                    </small>
                </div>
            </div>
        `;
    }

    updateSelectedTags() {
        this.selectedTags = {};

        // Get all enabled tag containers
        const enabledTags = this.container.querySelectorAll('input[id^="tag-"]:checked');
        
        enabledTags.forEach(checkbox => {
            const tagKey = checkbox.dataset.tag;
            const modeSelect = this.base.$(`#mode-${tagKey}`);
            const mode = modeSelect ? modeSelect.value : 'multi';
            
            if (mode === 'expression') {
                // Expression mode
                const expressionInput = this.base.$(`#expression-input-${tagKey}`);
                const expression = expressionInput ? expressionInput.value.trim() : '';
                
                if (expression) {
                    this.selectedTags[tagKey] = expression;
                }
            } else {
                // Multi-select mode
                const valueCheckboxes = this.container.querySelectorAll(`input[data-tag="${tagKey}"]:checked:not([id^="tag-"])`);
                const selectedValues = [];
                
                let hasAll = false;
                valueCheckboxes.forEach(cb => {
                    if (cb.value === 'ALL') {
                        hasAll = true;
                    } else {
                        selectedValues.push(cb.value);
                    }
                });
                
                if (hasAll) {
                    // If "All" is selected, don't add individual values
                    // The backend will handle this as no filter
                } else if (selectedValues.length > 0) {
                    this.selectedTags[tagKey] = selectedValues.join(',');
                }
            }

            // Update UI based on mode
            this.updateTagUI(tagKey, mode, checkbox.checked);
        });

        // Trigger callback
        if (this.onTagsChanged) {
            this.onTagsChanged(this.selectedTags);
        }
    }

    updateTagUI(tagKey, mode, isEnabled) {
        const valuesContainer = this.base.$(`#values-${tagKey}`);
        const expressionContainer = this.base.$(`#expression-${tagKey}`);
        const modeSelect = this.base.$(`#mode-${tagKey}`);
        const toggleIndicator = this.base.$(`.tag-header-clickable[data-tag="${tagKey}"] .toggle-indicator`);
        
        if (modeSelect) modeSelect.disabled = !isEnabled;
        
        if (valuesContainer && expressionContainer) {
            valuesContainer.style.display = isEnabled && mode === 'multi' ? 'block' : 'none';
            expressionContainer.style.display = isEnabled && mode === 'expression' ? 'block' : 'none';
            
            // Update toggle indicator
            if (toggleIndicator) {
                toggleIndicator.textContent = isEnabled ? '▼' : '▶';
            }
        }

        if (!isEnabled) {
            // Clear selections when disabled
            this.clearTagSelections(tagKey);
            
            // Update toggle indicator for disabled state
            if (toggleIndicator) {
                toggleIndicator.textContent = '▶';
            }
        }

        // Handle "All" checkbox logic (only add event listener once)
        const allCheckbox = this.base.$(`#value-${tagKey}-ALL`);
        if (allCheckbox && !allCheckbox.hasAttribute('data-listener-added')) {
            allCheckbox.setAttribute('data-listener-added', 'true');
            allCheckbox.addEventListener('change', () => {
                const isAllChecked = allCheckbox.checked;
                const valueCheckboxes = this.container.querySelectorAll(`input[data-tag="${tagKey}"]:not([value="ALL"])`);
                
                valueCheckboxes.forEach(cb => {
                    cb.checked = isAllChecked;
                });
                
                this.updateSelectedTags();
            });
        }
    }

    clearTagSelections(tagKey) {
        // Clear all checkboxes for this tag
        const checkboxes = this.container.querySelectorAll(`input[data-tag="${tagKey}"]`);
        checkboxes.forEach(cb => {
            if (!cb.id.startsWith('tag-')) { // Don't clear the main tag checkbox
                cb.checked = false;
            }
        });

        // Clear expression input
        const expressionInput = this.base.$(`#expression-input-${tagKey}`);
        if (expressionInput) {
            expressionInput.value = '';
        }
    }

    clearTags() {
        this.container.innerHTML = '';
        this.selectedTags = {};
        this.availableTags = {};
        
        if (this.onTagsChanged) {
            this.onTagsChanged(this.selectedTags);
        }
    }

    getSelectedTags() {
        return { ...this.selectedTags };
    }

    // Utility methods
    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    escapeId(text) {
        return text.replace(/[^a-zA-Z0-9_-]/g, '_');
    }
    
    /**
     * Handle value checkbox change - auto-check parent when value is selected
     */
    handleValueCheckboxChange(checkbox) {
        const tagKey = checkbox.dataset.tag;
        const mainCheckbox = this.base.$(`#tag-${tagKey}`);
        
        if (checkbox.checked && mainCheckbox && !mainCheckbox.checked) {
            // Auto-check the parent tag when a value is selected
            mainCheckbox.checked = true;
            
            // Update the UI to show the values container and enable mode select
            const modeSelect = this.base.$(`#mode-${tagKey}`);
            const mode = modeSelect ? modeSelect.value : 'multi';
            this.updateTagUI(tagKey, mode, true);
        }
        
        // Check if we should uncheck the parent (when no values are selected)
        if (!checkbox.checked) {
            const allValueCheckboxes = this.container.querySelectorAll(`input[data-tag="${tagKey}"]:not([id^="tag-"])`);
            const hasAnyChecked = Array.from(allValueCheckboxes).some(cb => cb.checked);
            
            if (!hasAnyChecked && mainCheckbox) {
                mainCheckbox.checked = false;
                const modeSelect = this.base.$(`#mode-${tagKey}`);
                const mode = modeSelect ? modeSelect.value : 'multi';
                this.updateTagUI(tagKey, mode, false);
            }
        }
    }
    
    /**
     * Toggle tag section visibility
     */
    toggleTagSection(headerElement) {
        const tagKey = headerElement.dataset.tag;
        const valuesContainer = this.base.$(`#values-${tagKey}`);
        const expressionContainer = this.base.$(`#expression-${tagKey}`);
        const toggleIndicator = headerElement.querySelector('.toggle-indicator');
        const mainCheckbox = this.base.$(`#tag-${tagKey}`);
        
        if (!valuesContainer && !expressionContainer) return;
        
        // Determine current visibility
        const modeSelect = this.base.$(`#mode-${tagKey}`);
        const mode = modeSelect ? modeSelect.value : 'multi';
        const currentContainer = mode === 'multi' ? valuesContainer : expressionContainer;
        const isCurrentlyVisible = currentContainer && currentContainer.style.display !== 'none';
        
        // Only toggle if the main checkbox is checked
        if (mainCheckbox && mainCheckbox.checked) {
            if (valuesContainer) {
                valuesContainer.style.display = (isCurrentlyVisible || mode !== 'multi') ? 'none' : 'block';
            }
            if (expressionContainer) {
                expressionContainer.style.display = (isCurrentlyVisible || mode !== 'expression') ? 'none' : 'block';
            }
            
            // Update toggle indicator
            if (toggleIndicator) {
                toggleIndicator.textContent = isCurrentlyVisible ? '▶' : '▼';
            }
        } else if (mainCheckbox) {
            // If not checked, check it and show the container
            mainCheckbox.checked = true;
            this.updateTagUI(tagKey, mode, true);
            this.updateSelectedTags();
        }
    }
}

// Make it globally available
window.TagFilters = TagFilters;