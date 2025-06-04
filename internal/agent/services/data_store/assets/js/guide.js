// SenHub Agent - Guide JavaScript

/**
 * Enhanced Markdown to HTML parser for the user guide
 */
class MarkdownParser {
    constructor() {
        this.toc = [];
    }

    parse(markdown) {
        this.toc = [];
        let html = markdown;

        // Split into blocks (paragraphs, code blocks, etc.)
        const blocks = this.splitIntoBlocks(html);
        html = this.processBlocks(blocks);

        return html;
    }

    splitIntoBlocks(text) {
        // Split by double newlines to get blocks
        const blocks = text.split(/\n\s*\n/);
        return blocks.filter(block => block.trim().length > 0);
    }

    processBlocks(blocks) {
        let html = '';
        let inCodeBlock = false;
        let codeLanguage = '';
        let codeContent = '';

        for (let block of blocks) {
            block = block.trim();
            
            // Handle code blocks
            if (block.startsWith('```')) {
                if (!inCodeBlock) {
                    // Start of code block
                    inCodeBlock = true;
                    codeLanguage = block.substring(3).trim();
                    codeContent = '';
                    continue;
                } else {
                    // End of code block
                    inCodeBlock = false;
                    html += `<pre><code class="language-${codeLanguage}">${this.escapeHtml(codeContent.trim())}</code></pre>\n\n`;
                    continue;
                }
            }

            if (inCodeBlock) {
                codeContent += block + '\n';
                continue;
            }

            // Process regular blocks
            html += this.processBlock(block) + '\n\n';
        }

        return html;
    }

    processBlock(block) {
        // Headers
        if (block.match(/^#{1,6}\s/)) {
            return this.processHeaders(block);
        }

        // Lists
        if (block.match(/^[\-\*\+]\s/) || block.match(/^\d+\.\s/)) {
            return this.processList(block);
        }

        // Regular paragraph
        return '<p>' + this.processInlineElements(block) + '</p>';
    }

    processHeaders(block) {
        const match = block.match(/^(#{1,6})\s(.+)/);
        if (!match) return block;

        const level = match[1].length;
        const title = match[2].trim();
        const id = this.generateId(title);
        
        this.toc.push({ level: level, title: title, id: id });
        
        return `<h${level} id="${id}">${this.processInlineElements(title)}</h${level}>`;
    }

    processList(block) {
        const lines = block.split('\n');
        let html = '';
        let inList = false;
        let listType = '';

        for (let line of lines) {
            line = line.trim();
            if (!line) continue;

            // Detect list type
            if (line.match(/^[\-\*\+]\s/)) {
                if (!inList || listType !== 'ul') {
                    if (inList && listType === 'ol') html += '</ol>';
                    if (!inList) html += '<ul>';
                    inList = true;
                    listType = 'ul';
                }
                const content = line.replace(/^[\-\*\+]\s/, '');
                html += `<li>${this.processInlineElements(content)}</li>`;
            } else if (line.match(/^\d+\.\s/)) {
                if (!inList || listType !== 'ol') {
                    if (inList && listType === 'ul') html += '</ul>';
                    if (!inList) html += '<ol>';
                    inList = true;
                    listType = 'ol';
                }
                const content = line.replace(/^\d+\.\s/, '');
                html += `<li>${this.processInlineElements(content)}</li>`;
            }
        }

        if (inList) {
            html += listType === 'ul' ? '</ul>' : '</ol>';
        }

        return html;
    }

    processInlineElements(text) {
        // Bold
        text = text.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>');
        
        // Italic
        text = text.replace(/\*(.*?)\*/g, '<em>$1</em>');
        
        // Inline code
        text = text.replace(/`([^`]+)`/g, '<code>$1</code>');
        
        // Links
        text = text.replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2" target="_blank">$1</a>');
        
        return text;
    }

    generateId(title) {
        return title
            .toLowerCase()
            .replace(/[^a-z0-9\s-]/g, '')
            .replace(/\s+/g, '-')
            .replace(/-+/g, '-')
            .replace(/^-|-$/g, '');
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    generateTOC() {
        if (this.toc.length === 0) return '';
        
        let tocHtml = '';
        
        this.toc.forEach(item => {
            const levelClass = `toc-level-${item.level}`;
            tocHtml += `<li><a href="#${item.id}" class="${levelClass}" data-target="${item.id}">${item.title}</a></li>`;
        });
        
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
        this.activeSection = null;
        
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
            
            // Setup interactions
            this.setupSmoothScrolling();
            this.setupScrollSpy();
            
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
                const targetId = link.getAttribute('data-target');
                const targetElement = document.getElementById(targetId);
                
                if (targetElement) {
                    // Remove active class from all links
                    tocLinks.forEach(l => l.classList.remove('active'));
                    // Add active class to clicked link
                    link.classList.add('active');
                    
                    // Smooth scroll to target
                    targetElement.scrollIntoView({
                        behavior: 'smooth',
                        block: 'start'
                    });
                    
                    this.activeSection = targetId;
                }
            });
        });
    }

    setupScrollSpy() {
        // Intersection Observer for scroll spy
        const headings = this.guideContent.querySelectorAll('h1, h2, h3, h4, h5, h6');
        const tocLinks = this.tocList.querySelectorAll('a[data-target]');
        
        if (headings.length === 0 || tocLinks.length === 0) return;

        const observer = new IntersectionObserver((entries) => {
            entries.forEach(entry => {
                if (entry.isIntersecting) {
                    const id = entry.target.id;
                    
                    // Remove active class from all TOC links
                    tocLinks.forEach(link => link.classList.remove('active'));
                    
                    // Add active class to corresponding TOC link
                    const activeLink = this.tocList.querySelector(`a[data-target="${id}"]`);
                    if (activeLink) {
                        activeLink.classList.add('active');
                    }
                }
            });
        }, {
            rootMargin: '-20% 0px -70% 0px',
            threshold: 0
        });

        headings.forEach(heading => {
            if (heading.id) {
                observer.observe(heading);
            }
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