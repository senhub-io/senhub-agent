// SenHub Agent - Tag Filters Module

/**
 * Advanced tag filtering with multi-selection support.
 * Separates category tags (rendered as pills) from resource tags (collapsible sections).
 */
class TagFilters {
    constructor(base, container, onTagsChanged) {
        this.base = base;
        this.container = container;
        this.onTagsChanged = onTagsChanged;
        this.selectedTags = {};
        this.availableTags = {};
        this.categories = [];
        this.activeCategories = {}; // tagKey -> Set of active values

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

        // Add click handlers for toggling tag sections and category pills
        this.container.addEventListener('click', (e) => {
            // Category pill click
            const pill = e.target.closest('.category-pill');
            if (pill) {
                this.handlePillClick(pill);
                return;
            }

            const header = e.target.closest('.tag-header-clickable');
            if (header) {
                this.toggleTagSection(header);
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
            this.categories = data.categories || [];

            this.renderTags();
            this.updateSelectedTags();

        } catch (error) {
            this.base.showError(this.container, `Failed to load tags: ${error.message}`);
        }
    }

    renderTags() {
        // Separate tags by type
        const categoryTags = {};
        const resourceTags = {};

        Object.entries(this.availableTags).forEach(([tagKey, tagInfo]) => {
            const tagType = tagInfo.type || 'resource';
            const values = tagInfo.values || [];

            if (tagType === 'category') {
                // Always show category tags (they control visibility of resource tags)
                if (values.length > 0) {
                    categoryTags[tagKey] = tagInfo;
                }
            } else {
                // Resource tags: hide if only 1 value (keeps existing behavior)
                if (values.length > 1) {
                    resourceTags[tagKey] = tagInfo;
                }
            }
        });

        const hasCategoryTags = Object.keys(categoryTags).length > 0;
        const hasResourceTags = Object.keys(resourceTags).length > 0;

        if (!hasCategoryTags && !hasResourceTags) {
            this.container.textContent = '';
            const notice = document.createElement('p');
            notice.className = 'info-notice';
            notice.textContent = 'No filterable tags available for this probe.';
            this.container.appendChild(notice);
            return;
        }

        // Initialize active categories (all active by default)
        this.activeCategories = {};
        Object.entries(categoryTags).forEach(([tagKey, tagInfo]) => {
            this.activeCategories[tagKey] = new Set(tagInfo.values || []);
        });

        // Build DOM using safe methods
        this.container.textContent = '';

        // Render category pills
        if (hasCategoryTags) {
            this.buildCategoryPills(categoryTags);
        }

        // Render resource tag filters
        if (hasResourceTags) {
            this.buildResourceFilters(resourceTags);
        }

        this.updateResourceVisibility();
    }

    /**
     * Build category tags as pill/toggle buttons using DOM methods
     */
    buildCategoryPills(categoryTags) {
        // Build category metadata lookup for metric counts
        const categoriesByKey = {};
        (this.categories || []).forEach(cat => {
            categoriesByKey[cat.key] = cat;
        });

        Object.entries(categoryTags).forEach(([tagKey, tagInfo]) => {
            const tagLabel = tagInfo.label || tagKey;
            const values = tagInfo.values || [];
            const valueLabels = tagInfo.value_labels || {};

            const selector = document.createElement('div');
            selector.className = 'category-selector';
            selector.dataset.tag = tagKey;

            const label = document.createElement('div');
            label.className = 'category-label';
            label.textContent = tagLabel;
            selector.appendChild(label);

            const pillsContainer = document.createElement('div');
            pillsContainer.className = 'category-pills';

            values.sort().forEach(value => {
                const displayLabel = valueLabels[value] || value;
                const catMeta = categoriesByKey[value];

                const btn = document.createElement('button');
                btn.className = 'category-pill active';
                btn.dataset.category = value;
                btn.dataset.tag = tagKey;
                btn.textContent = displayLabel;

                if (catMeta && catMeta.metric_count != null) {
                    const count = document.createElement('span');
                    count.className = 'pill-count';
                    count.textContent = catMeta.metric_count;
                    btn.appendChild(document.createTextNode(' '));
                    btn.appendChild(count);
                }

                pillsContainer.appendChild(btn);
            });

            selector.appendChild(pillsContainer);
            this.container.appendChild(selector);
        });
    }

    /**
     * Build resource tags as collapsible filter sections using DOM methods
     */
    buildResourceFilters(resourceTags) {
        const sortedTags = Object.entries(resourceTags).sort(([a], [b]) => a.localeCompare(b));

        const section = document.createElement('div');
        section.className = 'resource-filters';

        const sectionLabel = document.createElement('div');
        sectionLabel.className = 'resource-label';
        sectionLabel.textContent = 'Filter by resource';
        section.appendChild(sectionLabel);

        const grid = document.createElement('div');
        grid.className = 'tags-grid';

        sortedTags.forEach(([tagKey, tagInfo]) => {
            const filterEl = this.buildTagFilter(tagKey, tagInfo);
            grid.appendChild(filterEl);
        });

        section.appendChild(grid);
        this.container.appendChild(section);
    }

    /**
     * Build a single resource tag filter element using DOM methods
     */
    buildTagFilter(tagKey, tagInfo) {
        const values = tagInfo.values || [];
        const label = tagInfo.label || tagKey;
        const linkedCategories = tagInfo.linked_categories || [];
        const description = tagInfo.description && tagInfo.description !== "No description available"
            ? ` - ${tagInfo.description}`
            : '';
        const valueLabels = tagInfo.value_labels || {};

        const container = document.createElement('div');
        container.className = 'tag-filter-container';
        container.dataset.tag = tagKey;
        if (linkedCategories.length > 0) {
            container.dataset.linked = linkedCategories.join(',');
        }

        // Header
        const header = document.createElement('div');
        header.className = 'tag-header';

        const checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.id = `tag-${tagKey}`;
        checkbox.dataset.tag = tagKey;
        header.appendChild(checkbox);

        const clickable = document.createElement('div');
        clickable.className = 'tag-header-clickable';
        clickable.dataset.tag = tagKey;

        const headerLabel = document.createElement('label');
        headerLabel.htmlFor = `tag-${tagKey}`;
        headerLabel.textContent = `${label} (${values.length} values)${description}`;
        clickable.appendChild(headerLabel);

        const toggle = document.createElement('span');
        toggle.className = 'toggle-indicator';
        toggle.textContent = '\u25B6';
        clickable.appendChild(toggle);
        header.appendChild(clickable);

        const modeSelect = document.createElement('select');
        modeSelect.className = 'mode-select';
        modeSelect.id = `mode-${tagKey}`;
        modeSelect.disabled = true;
        const opt1 = document.createElement('option');
        opt1.value = 'multi';
        opt1.textContent = 'Multi-select';
        const opt2 = document.createElement('option');
        opt2.value = 'expression';
        opt2.textContent = 'Expression';
        modeSelect.appendChild(opt1);
        modeSelect.appendChild(opt2);
        header.appendChild(modeSelect);

        container.appendChild(header);

        // Values container
        const valuesContainer = document.createElement('div');
        valuesContainer.className = 'values-container';
        valuesContainer.id = `values-${tagKey}`;
        valuesContainer.style.display = 'none';

        // "All values" option
        const allOption = document.createElement('div');
        allOption.className = 'value-option';
        const allCb = document.createElement('input');
        allCb.type = 'checkbox';
        allCb.id = `value-${tagKey}-ALL`;
        allCb.value = 'ALL';
        allCb.dataset.tag = tagKey;
        const allLabel = document.createElement('label');
        allLabel.htmlFor = `value-${tagKey}-ALL`;
        allLabel.className = 'all-values-label';
        allLabel.textContent = 'All values';
        allOption.appendChild(allCb);
        allOption.appendChild(allLabel);
        valuesContainer.appendChild(allOption);

        // Individual values
        values.sort().forEach(value => {
            const vLabel = valueLabels[value] || value;
            const vOption = document.createElement('div');
            vOption.className = 'value-option';
            const vCb = document.createElement('input');
            vCb.type = 'checkbox';
            vCb.id = `value-${tagKey}-${this.escapeId(value)}`;
            vCb.value = value;
            vCb.dataset.tag = tagKey;
            const vLbl = document.createElement('label');
            vLbl.htmlFor = `value-${tagKey}-${this.escapeId(value)}`;
            vLbl.textContent = vLabel;
            vOption.appendChild(vCb);
            vOption.appendChild(vLbl);
            valuesContainer.appendChild(vOption);
        });

        container.appendChild(valuesContainer);

        // Expression container
        const exprContainer = document.createElement('div');
        exprContainer.className = 'expression-container';
        exprContainer.id = `expression-${tagKey}`;
        exprContainer.style.display = 'none';

        const exprInput = document.createElement('input');
        exprInput.type = 'text';
        exprInput.className = 'expression-input';
        exprInput.id = `expression-input-${tagKey}`;
        exprInput.placeholder = 'e.g: value1,value2 or value* or [0-9]+';
        exprInput.dataset.tag = tagKey;
        exprContainer.appendChild(exprInput);

        const exprHelp = document.createElement('small');
        exprHelp.className = 'expression-help';
        exprHelp.textContent = 'Supports: comma-separated values, wildcards (*), regex patterns';
        exprContainer.appendChild(exprHelp);

        container.appendChild(exprContainer);

        return container;
    }

    /**
     * Handle category pill click: toggle active/inactive state
     */
    handlePillClick(pill) {
        const tagKey = pill.dataset.tag;
        const categoryValue = pill.dataset.category;

        pill.classList.toggle('active');

        // Update activeCategories tracking
        if (!this.activeCategories[tagKey]) {
            this.activeCategories[tagKey] = new Set();
        }

        if (pill.classList.contains('active')) {
            this.activeCategories[tagKey].add(categoryValue);
        } else {
            this.activeCategories[tagKey].delete(categoryValue);
        }

        this.updateResourceVisibility();
        this.updateSelectedTags();
    }

    /**
     * Returns which category values are currently active, keyed by tag.
     * @returns {Object} e.g. { metric_type: ["sessions", "infrastructure"] }
     */
    getActiveCategories() {
        const result = {};
        Object.entries(this.activeCategories).forEach(([tagKey, valueSet]) => {
            if (valueSet.size > 0) {
                result[tagKey] = Array.from(valueSet);
            }
        });
        return result;
    }

    /**
     * Show/hide resource filters based on active category pills.
     * A resource filter is visible if:
     *   - it has no linked_categories (always visible), OR
     *   - at least one of its linked_categories overlaps with active category values
     */
    updateResourceVisibility() {
        const allActiveValues = new Set();
        Object.values(this.activeCategories).forEach(valueSet => {
            valueSet.forEach(v => allActiveValues.add(v));
        });

        const resourceContainers = this.container.querySelectorAll('.tag-filter-container[data-linked]');
        resourceContainers.forEach(container => {
            const linked = container.getAttribute('data-linked');
            if (!linked) return;

            const linkedList = linked.split(',');
            const isVisible = linkedList.some(cat => allActiveValues.has(cat));

            container.style.display = isVisible ? '' : 'none';

            // If hidden, uncheck and clear selections to avoid stale filters
            if (!isVisible) {
                const tagKey = container.dataset.tag;
                const mainCheckbox = this.base.$(`#tag-${tagKey}`);
                if (mainCheckbox && mainCheckbox.checked) {
                    mainCheckbox.checked = false;
                    this.clearTagSelections(tagKey);
                }
            }
        });

        // Also check if the resource-filters section is entirely empty
        const resourceSection = this.container.querySelector('.resource-filters');
        if (resourceSection) {
            const allContainers = resourceSection.querySelectorAll('.tag-filter-container');
            const anyVisible = Array.from(allContainers).some(c => c.style.display !== 'none');
            resourceSection.style.display = anyVisible ? '' : 'none';
        }
    }

    updateSelectedTags() {
        this.selectedTags = {};

        // Include active category pill selections as tags
        Object.entries(this.activeCategories).forEach(([tagKey, valueSet]) => {
            const tagInfo = this.availableTags[tagKey];
            if (!tagInfo) return;
            const allValues = tagInfo.values || [];

            // Only include if not all values are active (all active = no filter needed)
            if (valueSet.size > 0 && valueSet.size < allValues.length) {
                this.selectedTags[tagKey] = Array.from(valueSet).join(',');
            }
        });

        // Get all enabled resource tag containers
        const enabledTags = this.container.querySelectorAll('input[id^="tag-"]:checked');

        enabledTags.forEach(checkbox => {
            const tagKey = checkbox.dataset.tag;
            const tagContainer = this.container.querySelector(`.tag-filter-container[data-tag="${tagKey}"]`);
            if (tagContainer && tagContainer.style.display === 'none') return;
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
                    const allValueCheckboxes = this.container.querySelectorAll(`input[data-tag="${tagKey}"]:checked:not([value="ALL"])`);
                    const allValues = [];
                    allValueCheckboxes.forEach(cb => {
                        if (cb.id && !cb.id.startsWith('tag-')) {
                            allValues.push(cb.value);
                        }
                    });
                    if (allValues.length > 0) {
                        this.selectedTags[tagKey] = allValues.join(',');
                    }
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
                toggleIndicator.textContent = isEnabled ? '\u25BC' : '\u25B6';
            }
        }

        if (!isEnabled) {
            // Clear selections when disabled
            this.clearTagSelections(tagKey);

            // Update toggle indicator for disabled state
            if (toggleIndicator) {
                toggleIndicator.textContent = '\u25B6';
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
        this.container.textContent = '';
        this.selectedTags = {};
        this.availableTags = {};
        this.categories = [];
        this.activeCategories = {};

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
                toggleIndicator.textContent = isCurrentlyVisible ? '\u25B6' : '\u25BC';
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
