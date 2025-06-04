// SenHub Agent - Guide JavaScript

/**
 * Simple Markdown to HTML parser for the user guide
 */
class MarkdownParser {
    constructor() {
        this.toc = [];
    }

    parse(markdown) {
        this.toc = [];
        let html = markdown;

        // Headers (avec génération de TOC)
        html = html.replace(/^### (.*$)/gim, (match, title) => {
            const id = this.generateId(title);
            this.toc.push({ level: 3, title: title, id: id });
            return `<h3 id="${id}">${title}</h3>`;
        });

        html = html.replace(/^## (.*$)/gim, (match, title) => {
            const id = this.generateId(title);
            this.toc.push({ level: 2, title: title, id: id });
            return `<h2 id="${id}">${title}</h2>`;
        });

        html = html.replace(/^# (.*$)/gim, (match, title) => {
            const id = this.generateId(title);
            this.toc.push({ level: 1, title: title, id: id });
            return `<h1 id="${id}">${title}</h1>`;
        });

        // Code blocks
        html = html.replace(/```(\w+)?\n([\s\S]*?)```/gim, (match, language, code) => {
            return `<pre><code class="language-${language || 'text'}">${this.escapeHtml(code.trim())}</code></pre>`;
        });

        // Inline code
        html = html.replace(/`([^`]+)`/gim, '<code>$1</code>');

        // Bold
        html = html.replace(/\*\*(.*?)\*\*/gim, '<strong>$1</strong>');

        // Italic
        html = html.replace(/\*(.*?)\*/gim, '<em>$1</em>');

        // Links
        html = html.replace(/\[([^\]]+)\]\(([^)]+)\)/gim, '<a href="$2" target="_blank">$1</a>');

        // Lists
        html = html.replace(/^\- (.*$)/gim, '<li>$1</li>');
        html = html.replace(/(<li>.*<\/li>)/s, '<ul>$1</ul>');

        // Numbered lists
        html = html.replace(/^\d+\. (.*$)/gim, '<li>$1</li>');

        // Line breaks
        html = html.replace(/\n\n/gim, '<br><br>');
        html = html.replace(/\n/gim, '<br>');

        // Clean up nested lists
        html = html.replace(/<\/ul><br><ul>/gim, '');
        html = html.replace(/<\/ol><br><ol>/gim, '');

        return html;
    }

    generateId(title) {
        return title
            .toLowerCase()
            .replace(/[^a-z0-9\s-]/g, '')
            .replace(/\s+/g, '-')
            .replace(/-+/g, '-')
            .trim();
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    generateTOC() {
        let tocHtml = '';
        let currentLevel = 0;

        this.toc.forEach(item => {
            if (item.level > currentLevel) {
                tocHtml += '<ul>'.repeat(item.level - currentLevel);
            } else if (item.level < currentLevel) {
                tocHtml += '</ul>'.repeat(currentLevel - item.level);
            }
            
            tocHtml += `<li><a href="#${item.id}">${item.title}</a></li>`;
            currentLevel = item.level;
        });

        tocHtml += '</ul>'.repeat(currentLevel);
        return tocHtml;
    }
}

/**
 * Guide page main functionality
 */
class GuidePage {
    constructor(agentKey) {
        this.base = new SenHubBase(agentKey);
        this.parser = new MarkdownParser();
        
        this.initializeElements();
        this.loadGuide();
    }

    initializeElements() {
        // Status elements
        this.loadingDiv = this.base.$('#loading');
        this.errorDiv = this.base.$('#error');
        this.contentDiv = this.base.$('#content');
        
        // Content elements
        this.guideContent = this.base.$('#guide-content');
        this.tocList = this.base.$('#toc-list');
    }

    async loadGuide() {
        try {
            this.showLoading();
            
            // Load the markdown content
            const response = await fetch(`/web/${this.base.agentKey}/assets/USER_GUIDE.md`);
            if (!response.ok) {
                throw new Error(`HTTP ${response.status}: ${response.statusText}`);
            }
            
            const markdown = await response.text();
            
            // Parse markdown to HTML
            const html = this.parser.parse(markdown);
            this.guideContent.innerHTML = html;
            
            // Generate table of contents
            const tocHtml = this.parser.generateTOC();
            this.tocList.innerHTML = tocHtml;
            
            // Setup smooth scrolling for TOC links
            this.setupSmoothScrolling();
            
            this.showContent();
            
        } catch (error) {
            console.error('Failed to load guide:', error);
            this.showError();
        }
    }

    setupSmoothScrolling() {
        // Add smooth scrolling behavior to TOC links
        const tocLinks = this.tocList.querySelectorAll('a[href^="#"]');
        tocLinks.forEach(link => {
            link.addEventListener('click', (e) => {
                e.preventDefault();
                const targetId = link.getAttribute('href').substring(1);
                const targetElement = document.getElementById(targetId);
                
                if (targetElement) {
                    targetElement.scrollIntoView({
                        behavior: 'smooth',
                        block: 'start'
                    });
                }
            });
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
}

// Initialize when DOM is ready
document.addEventListener('DOMContentLoaded', () => {
    // Get agent key from template
    const agentKey = window.AGENT_KEY || document.querySelector('meta[name="agent-key"]')?.content;
    
    if (agentKey) {
        new GuidePage(agentKey);
    } else {
        console.error('Agent key not found');
        document.getElementById('loading').style.display = 'none';
        document.getElementById('error').style.display = 'block';
    }
});