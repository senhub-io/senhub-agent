// SenHub Agent - Base JavaScript utilities

/**
 * Base utilities and common functions
 */
class SenHubBase {
    constructor(agentKey) {
        this.agentKey = agentKey;
        this.baseUrl = `/api/${agentKey}`;
    }

    /**
     * Make API request with error handling
     */
    async fetchAPI(endpoint) {
        try {
            const response = await fetch(`${this.baseUrl}/${endpoint}`);
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }
            return await response.json();
        } catch (error) {
            console.error(`API Error (${endpoint}):`, error);
            throw error;
        }
    }

    /**
     * Show loading state
     */
    showLoading(element, message) {
        if (element) {
            element.textContent = '';
            const div = document.createElement('div');
            div.className = 'loading';
            div.textContent = message || 'Loading...';
            element.appendChild(div);
        }
    }

    /**
     * Show error state
     */
    showError(element, message) {
        if (element) {
            element.textContent = '';
            const div = document.createElement('div');
            div.className = 'error';
            div.textContent = 'Error: ' + message;
            element.appendChild(div);
        }
    }

    /**
     * Copy text to clipboard
     */
    async copyToClipboard(text) {
        try {
            await navigator.clipboard.writeText(text);
            return true;
        } catch (error) {
            console.error('Failed to copy to clipboard:', error);
            return false;
        }
    }

    /**
     * Debounce function calls
     */
    debounce(func, wait) {
        let timeout;
        return function executedFunction(...args) {
            const later = () => {
                clearTimeout(timeout);
                func(...args);
            };
            clearTimeout(timeout);
            timeout = setTimeout(later, wait);
        };
    }

    /**
     * DOM ready helper
     */
    ready(callback) {
        if (document.readyState !== 'loading') {
            callback();
        } else {
            document.addEventListener('DOMContentLoaded', callback);
        }
    }

    /**
     * Add event listener with cleanup
     */
    addEventListener(element, event, handler) {
        element.addEventListener(event, handler);
        return () => element.removeEventListener(event, handler);
    }

    /**
     * Query selector with error handling
     */
    $(selector, context = document) {
        const element = context.querySelector(selector);
        if (!element) {
            console.warn(`Element not found: ${selector}`);
        }
        return element;
    }

    /**
     * Query all selectors
     */
    $$(selector, context = document) {
        return context.querySelectorAll(selector);
    }
}

// Make it globally available
window.SenHubBase = SenHubBase;