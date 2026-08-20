package server

const appJavaScript = `(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => [...root.querySelectorAll(selector)];
  let drawing = null;
  let activeFragment = null;
  let selectedTool = 'select';
  let annotationColor = '#d04832';
  let diffLayout = 'inline';
  let selectionAnchor = null;
  let annotationDraft = null;
  let annotationDraftRedo = null;
  let selectedAnnotation = null;
  let annotationDrag = null;

  const languageKeywords = {
    go: new Set('break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var'.split(' ')),
    javascript: new Set('async await break case catch class const continue debugger default delete do else export extends finally for from function get if import in instanceof let new of return set static super switch this throw try typeof var void while with yield'.split(' ')),
    python: new Set('and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield'.split(' ')),
    ruby: new Set('alias and begin break case class def defined do else elsif end ensure false for if in module next nil not or redo rescue retry return self super then true undef unless until when while yield'.split(' ')),
    shell: new Set('case do done elif else esac fi for function if in select then time until while'.split(' ')),
    generic: new Set('class const enum false function interface let new null private public return static struct true type var void'.split(' '))
  };

  function normalizedAnnotationColor(value) {
    return /^#[0-9a-f]{6}$/i.test(value || '') ? value.toLowerCase() : '#d04832';
  }

  function colorWithAlpha(value, alpha = '55') {
    return normalizedAnnotationColor(value) + alpha;
  }

  function commandLabel(command) {
    return command?.label || 'annotation';
  }

  function updateHistoryControls() {
    const shapeDraft = annotationDraft?.shapeDraft;
    const undo = shapeDraft ? annotationDraft.undo.length && annotationDraft : annotationDraft;
    const redo = shapeDraft ? annotationDraft.redo.length && annotationDraft : annotationDraftRedo;
    qa('[data-undo]').forEach(button => {
      button.disabled = !undo;
      button.textContent = undo ? 'Undo ' + commandLabel(undo) : 'Undo';
      button.setAttribute('aria-label', undo ? 'Undo ' + commandLabel(undo) : 'Nothing to undo');
    });
    qa('[data-redo]').forEach(button => {
      button.disabled = !redo;
      button.textContent = redo ? 'Redo ' + commandLabel(redo) : 'Redo';
      button.setAttribute('aria-label', redo ? 'Redo ' + commandLabel(redo) : 'Nothing to redo');
    });
  }

  function submitThreadState(command, state) {
    const form = document.createElement('form');
    form.method = 'post';
    form.action = '/api/thread-state';
    const fields = {thread:command.thread, target:command.target, state, return_to:location.pathname + location.search + location.hash};
    Object.entries(fields).forEach(([name,value]) => {
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = name;
      input.value = value;
      form.append(input);
    });
    document.body.append(form);
    form.submit();
  }

  function shortcutDirection(event) {
    if (event.altKey || (!event.ctrlKey && !event.metaKey)) return '';
    const key = String(event.key || '').toLowerCase();
    if (key === 'z') return event.shiftKey ? 'redo' : 'undo';
    if (key === 'y' && event.ctrlKey && !event.shiftKey) return 'redo';
    return '';
  }

  async function copyPermalink(button) {
    const url = new URL(location.href);
    url.hash = (button.dataset.copyLink || '').replace(/^#/, '');
    try {
      if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable');
      await navigator.clipboard.writeText(url.toString());
    } catch (_) {
      const input = document.createElement('textarea');
      input.value = url.toString();
      input.setAttribute('readonly', '');
      input.style.position = 'fixed';
      input.style.opacity = '0';
      document.body.append(input);
      input.select();
      document.execCommand('copy');
      input.remove();
    }
    button.classList.add('copied');
    button.setAttribute('aria-label', 'Link copied');
    setTimeout(() => {
      button.classList.remove('copied');
      button.setAttribute('aria-label', 'Copy link');
    }, 1400);
  }

  function markExactText(target, exact, className = '', prefix = '', suffix = '') {
    if (!target || !exact) return null;
    const walker = document.createTreeWalker(target, NodeFilter.SHOW_TEXT);
    const nodes = [];
    let text = '';
    let node;
    while (node = walker.nextNode()) {
      nodes.push({node, start: text.length, end: text.length + node.data.length});
      text += node.data;
    }
    let offset = 0;
    while ((offset = text.indexOf(exact, offset)) >= 0) {
      const before = text.slice(0, offset);
      const after = text.slice(offset + exact.length);
      if ((!prefix || before.endsWith(prefix)) && (!suffix || after.startsWith(suffix))) {
        const first = nodes.find(item => item.end > offset);
        const last = nodes.find(item => item.end >= offset + exact.length);
        if (!first || !last) return null;
        const range = document.createRange();
        range.setStart(first.node, offset - first.start);
        range.setEnd(last.node, offset + exact.length - last.start);
        const mark = document.createElement('mark');
        if (className) mark.className = className;
        mark.append(range.extractContents());
        range.insertNode(mark);
        return mark;
      }
      offset += exact.length;
    }
    return null;
  }

  function cloneLandmarkAffordance(target) {
    return q('[data-landmark-affordance-template]', target)?.content.cloneNode(true) || null;
  }

  function positionLandmarkHotspots() {
    qa('.fragment-stage').forEach(stage => {
      const media = q('.fragment-frame,.fragment-image', stage);
      if (!media) return;
      const stageRect = stage.getBoundingClientRect();
      const mediaRect = media.getBoundingClientRect();
      qa('.landmark-hotspot', stage).forEach(visual => {
        visual.style.left = (mediaRect.left - stageRect.left + Number(visual.dataset.x) * mediaRect.width) + 'px';
        visual.style.top = (mediaRect.top - stageRect.top + Number(visual.dataset.y) * mediaRect.height) + 'px';
        visual.style.width = (Number(visual.dataset.width) * mediaRect.width) + 'px';
        visual.style.height = (Number(visual.dataset.height) * mediaRect.height) + 'px';
      });
    });
  }

  function prepareLandmarks() {
    qa('.fragment-frame').forEach(frame => {
      const aspect = Number(new URL(frame.src, location.href).searchParams.get('saga_aspect'));
      if (aspect > 0) {
        frame.style.minHeight = '0';
        frame.style.aspectRatio = String(aspect);
      }
      frame.addEventListener('load', positionLandmarkHotspots);
    });
    qa('.fragment-image').forEach(image => image.addEventListener('load', positionLandmarkHotspots));
    qa('[data-landmark-target]').forEach(target => {
      const anchor = target.dataset.landmarkAnchor;
      const fragment = target.closest('.fragment');
      if (!anchor || !fragment) return;
      if (target.dataset.landmarkType === 'heading') {
        const heading = document.getElementById(anchor);
        if (!heading) return;
        heading.querySelector('.heading-permalink')?.remove();
        const affordance = cloneLandmarkAffordance(target);
        if (affordance) heading.append(affordance);
      } else if (target.dataset.landmarkType === 'text') {
        const mark = markExactText(q('[data-selectable]', fragment), target.dataset.exact, 'content-landmark-text', target.dataset.prefix, target.dataset.suffix);
        if (!mark) return;
        target.removeAttribute('id');
        mark.id = anchor;
        mark.dataset.landmarkVisual = anchor;
        const affordance = cloneLandmarkAffordance(target);
        if (affordance) mark.append(affordance);
      }
    });
    globalThis.requestAnimationFrame?.(positionLandmarkHotspots);
  }

  function activateLandmark() {
    qa('[data-landmark-visual].active').forEach(element => element.classList.remove('active'));
    qa('.content-landmark-active').forEach(element => element.classList.remove('content-landmark-active'));
    const id = decodeURIComponent(location.hash.replace(/^#/, ''));
    const target = id ? q('[data-landmark-anchor="' + CSS.escape(id) + '"]') : null;
    if (!target) return;
    const fragment = target.closest('.fragment');
    if (!fragment) return;
    setActiveFragment(fragment);
    const visual = q('[data-landmark-visual="' + CSS.escape(id) + '"]', fragment);
    if (visual) {
      visual.classList.add('active');
      visual.classList.add('content-landmark-active');
    }
    if (target.dataset.landmarkType === 'element') {
      const frame = q('[data-fragment-frame]', fragment);
      if (!frame || !target.dataset.elementId) return;
      const base = frame.dataset.landmarkBase || frame.getAttribute('src').split('#')[0];
      frame.dataset.landmarkBase = base;
      const url = new URL(base, location.href);
      url.hash = target.dataset.elementId;
      if (frame.src !== url.toString()) frame.src = url.toString();
    }
    document.getElementById(id)?.scrollIntoView({block:'center'});
  }

  function languageForPath(path) {
    const name = (path || '').toLowerCase();
    const extension = name.includes('.') ? name.split('.').pop() : '';
    if (extension === 'go') return 'go';
    if (['js','jsx','mjs','cjs','ts','tsx'].includes(extension)) return 'javascript';
    if (extension === 'py') return 'python';
    if (extension === 'rb') return 'ruby';
    if (['sh','bash','zsh'].includes(extension)) return 'shell';
    if (['json','yaml','yml','toml','xml','html','css','scss','md','sql','c','h','cc','cpp','java','rs','swift','kt'].includes(extension)) return extension;
    return 'generic';
  }

  function tokenClass(token, language) {
    if (/^(\/\/|\/\*|\*|--)/.test(token) || (token.startsWith('#') && ['python','ruby','shell','yaml','yml'].includes(language))) return 'tok-comment';
    if (/^["'\x60]/.test(token)) return 'tok-string';
    if (/^\d/.test(token)) return 'tok-number';
    if (/^[{}()[\].,:;]+$/.test(token)) return 'tok-punctuation';
    const words = languageKeywords[language] || languageKeywords.generic;
    if (words.has(token) || languageKeywords.generic.has(token)) return 'tok-keyword';
    if (/^[A-Z][A-Za-z0-9_]*$/.test(token)) return 'tok-type';
    if (/^[A-Za-z_$][\w$-]*(?=\s*:)/.test(token)) return 'tok-property';
    return '';
  }

  function highlightCode(root = document) {
    qa('[data-code]', root).forEach(code => {
      if (code.dataset.highlighted) return;
      const path = (code.closest('[data-file-path]') || {}).dataset?.filePath || '';
      const language = languageForPath(path);
      const source = code.textContent;
      const pattern = /(\/\/.*$|\/\*[\s\S]*?\*\/|--.*$|#.*$|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\x60(?:\\.|[^\x60\\])*\x60|\b\d+(?:\.\d+)?\b|[A-Za-z_$][\w$-]*|[{}()[\].,:;]+)/gm;
      let offset = 0;
      const fragment = document.createDocumentFragment();
      for (const match of source.matchAll(pattern)) {
        if (match.index > offset) fragment.append(document.createTextNode(source.slice(offset, match.index)));
        const span = document.createElement('span');
        span.className = tokenClass(match[0], language);
        span.textContent = match[0];
        fragment.append(span);
        offset = match.index + match[0].length;
      }
      if (offset < source.length) fragment.append(document.createTextNode(source.slice(offset)));
      code.replaceChildren(fragment);
      code.dataset.highlighted = language;
    });
  }

  function prepareContext() {
    qa('[data-diff-body]').forEach(body => {
      const rows = [...body.children].filter(row => row.matches('.diff-row'));
      const hidden = rows.map((row, index) => {
        if (!row.matches('[data-context-row]')) return false;
        let before = Infinity, after = Infinity;
        for (let i = index - 1; i >= 0; i--) if (!rows[i].matches('[data-context-row]')) { before = index - i; break; }
        for (let i = index + 1; i < rows.length; i++) if (!rows[i].matches('[data-context-row]')) { after = i - index; break; }
        return before > 3 && after > 3;
      });
      for (let index = 0; index < rows.length;) {
        if (!hidden[index]) { index++; continue; }
        const group = [];
        while (index < rows.length && hidden[index]) { rows[index].hidden = true; group.push(rows[index]); index++; }
        const button = document.createElement('button');
        button.type = 'button';
        button.className = 'context-expander';
        button.textContent = group.length + ' unchanged lines · Expand';
        button.setAttribute('aria-label', 'Show ' + group.length + ' hidden unchanged lines');
        group[0].before(button);
        button.addEventListener('click', () => {
          group.forEach(row => { row.hidden = false; });
          button.remove();
          applyDiffLayout(diffLayout);
        });
      }
    });
  }

  function applyDiffLayout(mode) {
    diffLayout = mode === 'split' ? 'split' : 'inline';
    if (innerWidth <= 1050) diffLayout = 'inline';
    qa('[data-diff-surface]').forEach(surface => {
      surface.dataset.layout = diffLayout;
      const body = q('[data-diff-body]', surface);
      if (!body) return;
      qa(':scope > *', body).forEach(row => { row.style.gridRow = ''; });
      if (diffLayout !== 'split') return;
      const items = [...body.children].filter(item => !item.hidden);
      let gridRow = 1;
      for (let index = 0; index < items.length;) {
        const item = items[index];
        if (!item.matches('.diff-row.old')) {
          item.style.gridRow = String(gridRow++);
          index++;
          continue;
        }
        const oldRows = [];
        while (index < items.length && items[index].matches('.diff-row.old')) oldRows.push(items[index++]);
        const newRows = [];
        while (index < items.length && items[index].matches('.diff-row.new')) newRows.push(items[index++]);
        const count = Math.max(oldRows.length, newRows.length);
        oldRows.forEach((row, i) => { row.style.gridRow = String(gridRow + i); });
        newRows.forEach((row, i) => { row.style.gridRow = String(gridRow + i); });
        gridRow += count;
      }
    });
    qa('[data-layout]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.layout === diffLayout)));
  }

  function setTreeVisible(visible) {
    const shell = q('[data-shell]');
    if (shell) shell.classList.toggle('tree-hidden', !visible);
    qa('[data-toggle-tree]').forEach(button => button.setAttribute('aria-pressed', String(visible)));
  }

  function setRelatedVisible(visible) {
    const workspace = q('[data-code-workspace]');
    if (workspace) workspace.classList.toggle('related-hidden', !visible);
    qa('[data-toggle-related]').forEach(button => button.setAttribute('aria-pressed', String(visible)));
  }

  function filterTree() {
    const filter = (q('[data-file-filter]')?.value || '').trim().toLowerCase();
    const hideReviewed = Boolean(q('[data-hide-reviewed]')?.checked);
    const files = qa('[data-tree-file]');
    files.forEach(file => { file.hidden = !file.dataset.treePath.toLowerCase().includes(filter) || (hideReviewed && file.dataset.reviewed === 'true'); });
    qa('[data-tree-folder]').reverse().forEach(folder => { folder.hidden = !q('[data-tree-file]:not([hidden])', folder); });
    const empty = q('[data-tree-empty]');
    if (empty) empty.hidden = files.some(file => !file.hidden);
  }

  function selectedRangeURI(rows) {
    if (!rows.length) return '';
    const numbers = rows.map(row => Number(row.dataset.line));
    const uri = new URL(rows[0].dataset.diffRef);
    uri.searchParams.set('start', String(Math.min(...numbers)));
    uri.searchParams.set('end', String(Math.max(...numbers)));
    return uri.toString();
  }

  function updateLineSelection(rows) {
    const selected = new Set(rows);
    qa('[data-diff-row]').forEach(row => {
      row.classList.toggle('selected', selected.has(row));
      const button = q('[data-line-select]', row);
      if (button) button.setAttribute('aria-pressed', String(selected.has(row)));
    });
    const toolbar = q('[data-selection-toolbar]');
    if (!toolbar) return;
    toolbar.classList.toggle('open', rows.length > 0);
    if (!rows.length) { delete toolbar.dataset.diffRef; return; }
    toolbar.dataset.diffRef = selectedRangeURI(rows);
    toolbar.dataset.target = rows[0].dataset.target;
    toolbar.dataset.content = rows.map(row => q('[data-code]', row)?.textContent || '').join('\n');
    q('[data-selection-label]', toolbar).textContent = rows.length === 1 ? '1 line selected' : rows.length + ' lines selected';
  }

  function selectDiffLine(button, extend) {
    const row = button.closest('[data-diff-row]');
    if (!row) return;
    let rows = [row];
    if (extend && selectionAnchor && selectionAnchor.dataset.side === row.dataset.side && selectionAnchor.dataset.path === row.dataset.path) {
      const start = Number(selectionAnchor.dataset.line), end = Number(row.dataset.line);
      rows = qa('[data-diff-row]').filter(candidate => candidate.dataset.side === row.dataset.side && candidate.dataset.path === row.dataset.path && Number(candidate.dataset.line) >= Math.min(start,end) && Number(candidate.dataset.line) <= Math.max(start,end));
    } else {
      selectionAnchor = row;
    }
    updateLineSelection(rows);
  }

  function setView(name, updateURL = true) {
    if (!q('[data-view="'+name+'"]')) name = 'saga';
    qa('[data-view]').forEach(view => view.classList.toggle('active', view.dataset.view === name));
    qa('[data-view-tab]').forEach(tab => tab.classList.toggle('active', tab.dataset.viewTab === name));
    const sagaSide = q('.saga-side');
    const codeSide = q('.code-side');
    const toolbox = q('.annotation-toolbox');
    const codeMeta = q('.top-meta');
    if (sagaSide) sagaSide.hidden = name !== 'saga';
    if (codeSide) codeSide.hidden = name !== 'code';
    if (toolbox) toolbox.hidden = name !== 'saga';
    if (codeMeta) codeMeta.hidden = name !== 'code';
    const shell = q('[data-shell]');
    if (shell) shell.classList.toggle('code-mode', name === 'code');
    if (updateURL) {
      const url = new URL(location.href);
      if (name === 'saga') url.searchParams.delete('view'); else url.searchParams.set('view', name);
      history.pushState({view: name}, '', url);
    }
  }

  function filterManifest() {
    const input = q('[data-manifest-filter]');
    const query = (input?.value || '').trim().toLowerCase();
    qa('[data-manifest-panel]').forEach(panel => {
      let visible = 0;
      qa('[data-manifest-search]', panel).forEach(group => {
        const match = !query || group.dataset.manifestSearch.toLowerCase().includes(query);
        group.hidden = !match;
        if (match) visible++;
      });
      const empty = q('[data-manifest-empty]', panel);
      if (empty) empty.hidden = visible !== 0 || !query;
    });
  }

  function setManifestMode(mode) {
    qa('[data-manifest-mode]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.manifestMode === mode)));
    qa('[data-manifest-panel]').forEach(panel => panel.hidden = panel.dataset.manifestPanel !== mode);
    filterManifest();
  }

  function setActiveFragment(fragment) {
    if (!fragment || fragment === activeFragment) return;
    if (activeFragment) activeFragment.classList.remove('active-fragment');
    activeFragment = fragment;
    activeFragment.classList.add('active-fragment');
    const label = q('[data-tool-target]');
    if (label) label.textContent = fragment.dataset.fragmentTitle || 'Selected fragment';
  }

  function cancelDrawing() {
    if (!drawing) return;
    drawing.overlay.classList.remove('drawing');
    if (drawing.preview) drawing.preview.remove();
    drawing = null;
  }

  function setSelectedTool(mode) {
    selectedTool = mode;
    qa('[data-tool]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.tool === mode)));
  }

  function resetTool() {
    cancelDrawing();
    setSelectedTool('select');
  }

  function openDrawer(templateID) {
    const source = document.getElementById(templateID);
    if (!source) return;
    const body = q('.drawer-body');
    body.innerHTML = source.innerHTML;
    const attached = q('[data-attached-title]', body);
    const heading = q('.drawer-head strong');
    if (heading) heading.textContent = attached?.dataset.attachedTitle ? 'Linked code · ' + attached.dataset.attachedTitle : 'Linked code';
    highlightCode(body);
    q('.diff-drawer').classList.add('open');
    q('.diff-drawer').setAttribute('aria-hidden', 'false');
    q('.drawer-backdrop').classList.add('open');
    document.body.style.overflow = 'hidden';
    q('[data-close-drawer]', q('.diff-drawer')).focus();
  }

  function closeDrawer() {
    const drawer = q('.diff-drawer');
    if (!drawer) return;
    drawer.classList.remove('open');
    drawer.setAttribute('aria-hidden', 'true');
    q('.drawer-backdrop').classList.remove('open');
    document.body.style.overflow = '';
  }

  function openDiffComposer(button) {
    const form = q('.diff-compose');
    const suggestion = button.dataset.diffAction === 'suggestion';
	form.reset();
    form.classList.toggle('suggesting', suggestion);
    form.classList.add('open');
    q('[name=target]', form).value = button.dataset.target;
    q('[name=anchor]', form).value = JSON.stringify({type:'diff', diff:{uri:button.dataset.diffRef}});
    q('[name=kind]', form).value = suggestion ? 'suggestion' : 'comment';
    q('.diff-compose-head strong', form).textContent = suggestion ? 'Suggest a replacement' : 'Comment on this change';
    q('[name=replacement]', form).value = suggestion ? (button.dataset.content || '') : '';
    q('[name=body]', form).required = true;
    q('[name=body]', form).focus();
  }

  function annotationLabel(anchor) {
    if (anchor?.type === 'text') return 'highlight';
    if (anchor?.type === 'region') return 'rectangle';
    if (anchor?.type === 'drawing') return 'freehand';
    return 'comment';
  }

  function cloneAnchor(anchor) {
    return JSON.parse(JSON.stringify(anchor));
  }

  function stepShapeDraftHistory(draft, direction) {
    const from = direction === 'undo' ? draft.undo : draft.redo;
    const to = direction === 'undo' ? draft.redo : draft.undo;
    if (!from.length) return false;
    to.push(cloneAnchor(draft.anchor));
    draft.anchor = from.pop();
    return true;
  }

  function createShapeElement(shape, className, index) {
    const name = shape.type === 'rect' ? 'rect' : shape.type === 'ellipse' ? 'ellipse' : shape.type === 'line' ? 'line' : 'polyline';
    const element = document.createElementNS('http://www.w3.org/2000/svg', name);
    element.setAttribute('class', 'annotation selectable ' + className + (shape.type === 'path' || shape.type === 'line' ? ' path' : ''));
    element.dataset.shapeIndex = String(index);
    renderShapeElement(element, shape);
    return element;
  }

  function renderShapeElement(element, shape) {
    const color = normalizedAnnotationColor(shape.color);
    element.setAttribute('stroke', color);
    if (shape.type === 'rect') {
      element.setAttribute('fill', color);
      element.setAttribute('fill-opacity', '.22');
      element.setAttribute('x', shape.x * 1000);
      element.setAttribute('y', shape.y * 1000);
      element.setAttribute('width', shape.width * 1000);
      element.setAttribute('height', shape.height * 1000);
    } else if (shape.type === 'ellipse') {
      element.setAttribute('fill', color);
      element.setAttribute('fill-opacity', '.22');
      element.setAttribute('cx', shape.x * 1000);
      element.setAttribute('cy', shape.y * 1000);
      element.setAttribute('rx', shape.width * 1000);
      element.setAttribute('ry', shape.height * 1000);
    } else if (shape.type === 'line') {
      element.setAttribute('x1', shape.x * 1000);
      element.setAttribute('y1', shape.y * 1000);
      element.setAttribute('x2', shape.width * 1000);
      element.setAttribute('y2', shape.height * 1000);
    } else {
      element.setAttribute('points', (shape.points || []).map(point => point.x * 1000 + ',' + point.y * 1000).join(' '));
    }
  }

  function shapeFromElement(element) {
    const type = element.tagName.toLowerCase() === 'polyline' ? 'path' : element.tagName.toLowerCase();
    const shape = {type, color:normalizedAnnotationColor(element.getAttribute('stroke'))};
    if (type === 'rect') {
      Object.assign(shape, {x:Number(element.getAttribute('x'))/1000,y:Number(element.getAttribute('y'))/1000,width:Number(element.getAttribute('width'))/1000,height:Number(element.getAttribute('height'))/1000});
    } else if (type === 'ellipse') {
      Object.assign(shape, {x:Number(element.getAttribute('cx'))/1000,y:Number(element.getAttribute('cy'))/1000,width:Number(element.getAttribute('rx'))/1000,height:Number(element.getAttribute('ry'))/1000});
    } else if (type === 'line') {
      Object.assign(shape, {x:Number(element.getAttribute('x1'))/1000,y:Number(element.getAttribute('y1'))/1000,width:Number(element.getAttribute('x2'))/1000,height:Number(element.getAttribute('y2'))/1000});
    } else {
      shape.points = (element.getAttribute('points') || '').trim().split(/\s+/).filter(Boolean).map(value => {
        const [x,y] = value.split(',').map(Number);
        return {x:x/1000,y:y/1000};
      });
    }
    return shape;
  }

  function persistedAnchor(group) {
    return {type:group.dataset.anchorType,coordinate_space:'normalized',shapes:qa('[data-shape-index]', group).map(shapeFromElement)};
  }

  function clearAnnotationSelection() {
    q('.annotation.selected')?.classList.remove('selected');
    qa('[data-annotation-resize]').forEach(element => element.remove());
    selectedAnnotation = null;
    const tools = q('[data-annotation-selection]');
    if (tools) tools.hidden = true;
  }

  function updateAnnotationResizeHandle() {
    qa('[data-annotation-resize]').forEach(element => element.remove());
    if (!selectedAnnotation || selectedAnnotation.element.tagName.toLowerCase() !== 'rect') return;
    const element = selectedAnnotation.element;
    const handle = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
    handle.setAttribute('class', 'annotation-resize-handle');
    handle.dataset.annotationResize = 'true';
    handle.setAttribute('x', Number(element.getAttribute('x')) + Number(element.getAttribute('width')) - 7);
    handle.setAttribute('y', Number(element.getAttribute('y')) + Number(element.getAttribute('height')) - 7);
    handle.setAttribute('width', '14');
    handle.setAttribute('height', '14');
    element.parentNode.append(handle);
  }

  function selectAnnotation(element) {
    clearAnnotationSelection();
    const draft = element.classList.contains('pending');
    const group = element.closest('[data-annotation-entity]');
    selectedAnnotation = {
      kind:draft ? 'draft' : 'persisted',
      element,
      group,
      index:Number(element.dataset.shapeIndex),
      thread:group?.dataset.threadId || '',
      target:group?.dataset.target || '',
      state:group?.dataset.threadState || 'open'
    };
    element.classList.add('selected');
    const tools = q('[data-annotation-selection]');
    if (tools) tools.hidden = false;
    const color = normalizedAnnotationColor(element.getAttribute('stroke'));
    annotationColor = color;
    const picker = q('[data-annotation-color]');
    if (picker) picker.value = color;
    setSelectedTool('select');
    updateAnnotationResizeHandle();
  }

  function discardAnnotationDraft() {
    qa('.annotation.pending').forEach(element => element.remove());
    annotationDraft = null;
    annotationDraftRedo = null;
    clearAnnotationSelection();
  }

  function syncShapeDraft(showComposer = true) {
    if (!annotationDraft?.shapeDraft) return;
    const overlay = q('.review-overlay', annotationDraft.fragment);
    qa('.annotation.pending', overlay).forEach(element => element.remove());
    annotationDraft.anchor.shapes.forEach((shape,index) => overlay.append(createShapeElement(shape, 'pending', index)));
    annotationDraft.anchor.type = annotationDraft.anchor.shapes.some(shape => shape.type === 'path') ? 'drawing' : 'region';
    const form = q('.annotation-compose');
    q('[name=target]', form).value = annotationDraft.target;
    q('[name=anchor]', form).value = JSON.stringify(annotationDraft.anchor);
    form.classList.toggle('open', showComposer && annotationDraft.anchor.shapes.length > 0);
    clearAnnotationSelection();
    updateHistoryControls();
  }

  function ensureShapeDraft(fragment) {
    if (annotationDraft?.shapeDraft && annotationDraft.fragment === fragment) return annotationDraft;
    const form = q('.annotation-compose');
    discardAnnotationDraft();
    form.reset();
    annotationDraft = {
      kind:'draft', shapeDraft:true, target:fragment.dataset.target, fragment, body:'',
      anchor:{type:'region',coordinate_space:'normalized',shapes:[]},
      undo:[], redo:[], label:'annotation'
    };
    return annotationDraft;
  }

  function addDraftShape(fragment, shape) {
    const draft = ensureShapeDraft(fragment);
    draft.body = q('[name=body]', q('.annotation-compose'))?.value || draft.body || '';
    draft.undo.push(cloneAnchor(draft.anchor));
    draft.redo = [];
    draft.anchor.shapes.push(shape);
    syncShapeDraft(true);
    const form = q('.annotation-compose');
    q('[name=body]', form).value = draft.body;
    q('[name=body]', form).focus();
    setSelectedTool('select');
  }

  function openAnnotation(anchor, options = {}) {
    const fragment = options.fragment || activeFragment;
    if (!fragment) return;
    discardAnnotationDraft();
    setActiveFragment(fragment);
    const form = q('.annotation-compose');
    form.reset();
    q('[name=target]', form).value = fragment.dataset.target;
    q('[name=anchor]', form).value = JSON.stringify(anchor);
    q('[name=body]', form).value = options.body || '';
    form.classList.add('open');
    q('[name=body]', form).focus();
    annotationDraft = {
      kind:'draft',
      anchor,
      target:fragment.dataset.target,
      fragment,
      body:options.body || '',
      label:annotationLabel(anchor)
    };
    if (!options.fromRedo) annotationDraftRedo = null;
    resetTool();
    updateHistoryControls();
  }

  function closeAnnotation(discard = true) {
    const form = q('.annotation-compose');
    if (form) form.classList.remove('open');
    if (discard) {
      discardAnnotationDraft();
    }
    resetTool();
    updateHistoryControls();
  }

  function undoDraft() {
    if (!annotationDraft) return false;
    const form = q('.annotation-compose');
    annotationDraft.body = q('[name=body]', form)?.value || '';
    if (annotationDraft.shapeDraft) {
      if (!stepShapeDraftHistory(annotationDraft, 'undo')) return false;
      syncShapeDraft(true);
      q('[name=body]', form).value = annotationDraft.body;
      return true;
    }
    form?.classList.remove('open');
    annotationDraftRedo = annotationDraft;
    annotationDraft = null;
    resetTool();
    updateHistoryControls();
    return true;
  }

  function redoDraft() {
    if (annotationDraft?.shapeDraft) {
      if (!stepShapeDraftHistory(annotationDraft, 'redo')) return false;
      syncShapeDraft(true);
      q('[name=body]', q('.annotation-compose')).value = annotationDraft.body || '';
      return true;
    }
    if (!annotationDraftRedo) return false;
    const draft = annotationDraftRedo;
    annotationDraftRedo = null;
    openAnnotation(draft.anchor, {...draft, fromRedo:true});
    return true;
  }

  function performHistoryAction(direction) {
    if (direction === 'undo') undoDraft();
    else redoDraft();
  }

  function shapeBounds(shape) {
    if (shape.type === 'path') {
      const xs = shape.points.map(point => point.x), ys = shape.points.map(point => point.y);
      return {left:Math.min(...xs),right:Math.max(...xs),top:Math.min(...ys),bottom:Math.max(...ys)};
    }
    if (shape.type === 'line') {
      return {left:Math.min(shape.x,shape.width),right:Math.max(shape.x,shape.width),top:Math.min(shape.y,shape.height),bottom:Math.max(shape.y,shape.height)};
    }
    if (shape.type === 'ellipse') {
      return {left:shape.x-shape.width,right:shape.x+shape.width,top:shape.y-shape.height,bottom:shape.y+shape.height};
    }
    return {left:shape.x,right:shape.x+shape.width,top:shape.y,bottom:shape.y+shape.height};
  }

  function translateShape(shape, dx, dy) {
    const copy = JSON.parse(JSON.stringify(shape));
    const bounds = shapeBounds(copy);
    dx = Math.max(-bounds.left, Math.min(1-bounds.right, dx));
    dy = Math.max(-bounds.top, Math.min(1-bounds.bottom, dy));
    if (copy.type === 'path') {
      copy.points = copy.points.map(point => ({x:point.x+dx,y:point.y+dy}));
    } else {
      copy.x += dx;
      copy.y += dy;
      if (copy.type === 'line') {
        copy.width += dx;
        copy.height += dy;
      }
    }
    return copy;
  }

  async function persistAnnotationAnchor(thread, anchor) {
    const response = await fetch('/api/thread-anchor', {
      method:'POST',
      headers:{'Content-Type':'application/x-www-form-urlencoded'},
      body:new URLSearchParams({thread,anchor:JSON.stringify(anchor)})
    });
    if (!response.ok) throw new Error(await response.text());
  }

  function selectedAnchor() {
    if (!selectedAnnotation) return null;
    return selectedAnnotation.kind === 'draft' ? cloneAnchor(annotationDraft.anchor) : persistedAnchor(selectedAnnotation.group);
  }

  async function recolorSelectedAnnotation(color) {
    if (!selectedAnnotation) return;
    const before = selectedAnchor();
    const anchor = cloneAnchor(before);
    anchor.shapes[selectedAnnotation.index].color = normalizedAnnotationColor(color);
    renderShapeElement(selectedAnnotation.element, anchor.shapes[selectedAnnotation.index]);
    if (selectedAnnotation.kind === 'draft') {
      annotationDraft.undo.push(before);
      annotationDraft.redo = [];
      annotationDraft.anchor = anchor;
      q('[name=anchor]', q('.annotation-compose')).value = JSON.stringify(anchor);
      updateHistoryControls();
      return;
    }
    try {
      await persistAnnotationAnchor(selectedAnnotation.thread, anchor);
    } catch (error) {
      renderShapeElement(selectedAnnotation.element, before.shapes[selectedAnnotation.index]);
      alert('Could not update annotation: ' + error.message);
    }
  }

  async function removeSelectedAnnotation() {
    if (!selectedAnnotation) return;
    const selection = selectedAnnotation;
    const before = selectedAnchor();
    if (selection.kind === 'draft') {
      annotationDraft.undo.push(before);
      annotationDraft.redo = [];
      annotationDraft.anchor.shapes.splice(selection.index, 1);
      syncShapeDraft(true);
      return;
    }
    if (before.shapes.length === 1) {
      submitThreadState({thread:selection.thread,target:selection.target}, 'withdrawn');
      return;
    }
    const anchor = cloneAnchor(before);
    anchor.shapes.splice(selection.index, 1);
    selection.element.remove();
    qa('[data-shape-index]', selection.group).forEach((element,index) => { element.dataset.shapeIndex = String(index); });
    clearAnnotationSelection();
    try {
      await persistAnnotationAnchor(selection.thread, anchor);
    } catch (error) {
      location.reload();
    }
  }

  function openDecision(button) {
    const dialog = q('.decision-dialog');
	q('form', dialog).reset();
    q('[name=target]', dialog).value = button.dataset.reviewTarget;
    q('#decision-title', dialog).textContent = 'Review ' + button.dataset.reviewTitle;
    if (!dialog.open) dialog.showModal();
    q('[name=body]', dialog).focus();
  }

  function useTool(mode) {
    cancelDrawing();
    clearAnnotationSelection();
    setSelectedTool(mode);
    if (mode === 'select') return;
    if (!activeFragment) {
      const label = q('[data-tool-target]');
      if (label) label.textContent = 'Select a fragment first';
      resetTool();
      return;
    }
    if (mode === 'target') {
      openAnnotation({type:'target'});
      return;
    }
    if (mode === 'text') {
      const selection = getSelection();
      const selectable = q('[data-selectable]', activeFragment);
      if (!selection || selection.isCollapsed || !selectable || !selectable.contains(selection.anchorNode) || !selectable.contains(selection.focusNode)) {
        const label = q('[data-tool-target]');
        if (label) label.textContent = 'Select text in the active fragment';
        resetTool();
        return;
      }
      const exact = selection.toString();
      const selectedRange = selection.getRangeAt(0);
      const before = document.createRange();
      before.selectNodeContents(selectable);
      before.setEnd(selectedRange.startContainer, selectedRange.startOffset);
      const start = before.toString().length;
      const allText = selectable.textContent;
      openAnnotation({type:'text',text:{exact,start,end:start+exact.length,prefix:allText.slice(Math.max(0,start-32),start),suffix:allText.slice(start+exact.length,start+exact.length+32),color:annotationColor}});
      return;
    }
    const overlay = q('.review-overlay', activeFragment);
    if (!overlay) {
      resetTool();
      return;
    }
    overlay.classList.add('drawing');
    drawing = {fragment: activeFragment, overlay, mode, color:annotationColor, points: []};
  }

  document.addEventListener('mousedown', event => {
    if (event.target.closest('[data-tool="text"]')) event.preventDefault();
  });

  document.addEventListener('pointerover', event => {
    const fragment = event.target.closest('.fragment');
    if (fragment && !drawing) setActiveFragment(fragment);
  });

  document.addEventListener('focusin', event => {
    const fragment = event.target.closest('.fragment');
    if (fragment) setActiveFragment(fragment);
  });

  document.addEventListener('click', event => {
    const permalink = event.target.closest('[data-copy-link]');
    if (permalink) { copyPermalink(permalink); return; }
    if (event.target.closest('[data-undo]')) { performHistoryAction('undo'); return; }
    if (event.target.closest('[data-redo]')) { performHistoryAction('redo'); return; }
    if (event.target.closest('[data-remove-annotation]')) { removeSelectedAnnotation(); return; }
    const annotation = event.target.closest('.annotation.selectable');
    if (annotation) { selectAnnotation(annotation); return; }
    const viewTab = event.target.closest('[data-view-tab]');
    if (viewTab) { setView(viewTab.dataset.viewTab); return; }
    const manifestMode = event.target.closest('[data-manifest-mode]');
    if (manifestMode) { setManifestMode(manifestMode.dataset.manifestMode); return; }
    const treeToggle = event.target.closest('[data-toggle-tree]');
    if (treeToggle) {
      const hidden = q('[data-shell]')?.classList.contains('tree-hidden');
      setTreeVisible(Boolean(hidden));
      if (hidden) setTimeout(() => q('[data-file-filter]')?.focus(), 0);
      else q('.code-toolbar [data-toggle-tree]')?.focus();
      return;
    }
    const relatedToggle = event.target.closest('[data-toggle-related]');
    if (relatedToggle) {
      const hidden = q('[data-code-workspace]')?.classList.contains('related-hidden');
      setRelatedVisible(Boolean(hidden));
      if (!hidden) q('.code-toolbar [data-toggle-related]')?.focus();
      return;
    }
    const layout = event.target.closest('[data-layout]');
    if (layout) { applyDiffLayout(layout.dataset.layout); return; }
    const lineSelect = event.target.closest('[data-line-select]');
    if (lineSelect) { selectDiffLine(lineSelect, event.shiftKey); return; }
    const selectionAction = event.target.closest('[data-selection-action]');
    if (selectionAction) {
      const toolbar = selectionAction.closest('[data-selection-toolbar]');
      openDiffComposer({dataset:{diffAction:selectionAction.dataset.selectionAction,diffRef:toolbar.dataset.diffRef,target:toolbar.dataset.target,content:toolbar.dataset.content}});
      return;
    }
    if (event.target.closest('[data-selection-clear]')) { selectionAnchor = null; updateLineSelection([]); return; }
    const drawerButton = event.target.closest('[data-open-diffs]');
    if (drawerButton) { openDrawer(drawerButton.dataset.openDiffs); return; }
    if (event.target.closest('[data-close-drawer]')) { closeDrawer(); return; }
    const decisionButton = event.target.closest('[data-review-target]');
    if (decisionButton) { openDecision(decisionButton); return; }
    if (event.target.closest('[data-close-decision]')) { q('.decision-dialog').close(); return; }
    if (event.target.closest('[data-close-annotation]')) { closeAnnotation(); return; }
    const diffAction = event.target.closest('[data-diff-action]');
    if (diffAction) { openDiffComposer(diffAction); return; }
    if (event.target.closest('[data-close-diff-compose]')) { q('.diff-compose').classList.remove('open'); return; }
    const tool = event.target.closest('[data-tool]');
    if (tool) { useTool(tool.dataset.tool); return; }
    const fragment = event.target.closest('.fragment');
    if (fragment) setActiveFragment(fragment);
  });

  document.addEventListener('submit', event => {
    const form = event.target;
    if (form.matches('form[action^="/api/"]')) {
      let returnTo = q('[name=return_to]', form);
      if (!returnTo) {
        returnTo = document.createElement('input');
        returnTo.type = 'hidden';
        returnTo.name = 'return_to';
        form.append(returnTo);
      }
      returnTo.value = location.pathname + location.search + location.hash;
    }
  });

  document.addEventListener('keydown', event => {
    const direction = shortcutDirection(event);
    if (direction) {
      const editable = event.target.matches?.('input,textarea,[contenteditable="true"]');
      const emptyDraftBody = annotationDraft && event.target === q('[name=body]', q('.annotation-compose')) && !event.target.value;
      if (!editable || emptyDraftBody) {
        event.preventDefault();
        performHistoryAction(direction);
      }
      return;
    }
    const treeItem = event.target.closest('.file-tree [role=treeitem]');
    if (treeItem && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      const items = qa('.file-tree [role=treeitem]').filter(item => !item.hidden && !item.closest('[hidden]'));
      const index = items.indexOf(treeItem);
      const next = items[index + (event.key === 'ArrowDown' ? 1 : -1)];
      if (next) next.focus();
      event.preventDefault();
      return;
    }
    if (event.key !== 'Escape') return;
    if (selectedAnnotation) {
      clearAnnotationSelection();
      return;
    }
    closeDrawer();
    closeAnnotation();
    updateLineSelection([]);
    const diffForm = q('.diff-compose');
    if (diffForm) diffForm.classList.remove('open');
    const dialog = q('.decision-dialog');
    if (dialog && dialog.open) dialog.close();
  });

  document.addEventListener('pointerdown', event => {
    const resizeHandle = event.target.closest?.('[data-annotation-resize]');
    if (resizeHandle && selectedAnnotation) {
      const overlay = resizeHandle.closest('.review-overlay');
      const box = overlay.getBoundingClientRect();
      annotationDrag = {
        selection:selectedAnnotation,
        before:selectedAnchor(),
        start:{x:(event.clientX-box.left)/box.width,y:(event.clientY-box.top)/box.height},
        box,
        mode:'resize',
        moved:false
      };
      resizeHandle.setPointerCapture?.(event.pointerId);
      event.preventDefault();
      return;
    }
    const annotation = event.target.closest?.('.annotation.selectable');
    if (annotation && selectedTool === 'select') {
      selectAnnotation(annotation);
      const overlay = annotation.closest('.review-overlay');
      const box = overlay.getBoundingClientRect();
      annotationDrag = {
        selection:selectedAnnotation,
        before:selectedAnchor(),
        start:{x:(event.clientX-box.left)/box.width,y:(event.clientY-box.top)/box.height},
        box,
        mode:'move',
        moved:false
      };
      annotation.setPointerCapture?.(event.pointerId);
      event.preventDefault();
      return;
    }
    if (!drawing || event.target !== drawing.overlay) return;
    const box = drawing.overlay.getBoundingClientRect();
    drawing.points = [{x:(event.clientX-box.left)/box.width,y:(event.clientY-box.top)/box.height}];
    drawing.start = drawing.points[0];
    drawing.preview = document.createElementNS('http://www.w3.org/2000/svg', drawing.mode === 'rect' ? 'rect' : 'polyline');
    drawing.preview.setAttribute('class', 'annotation pending ' + (drawing.mode === 'draw' ? 'path' : ''));
    drawing.preview.style.setProperty('--active-annotation-color', drawing.color);
    drawing.overlay.append(drawing.preview);
    event.preventDefault();
  });

  document.addEventListener('pointermove', event => {
    if (annotationDrag) {
      const point = {x:(event.clientX-annotationDrag.box.left)/annotationDrag.box.width,y:(event.clientY-annotationDrag.box.top)/annotationDrag.box.height};
      const dx = point.x-annotationDrag.start.x, dy = point.y-annotationDrag.start.y;
      const anchor = cloneAnchor(annotationDrag.before);
      if (annotationDrag.mode === 'resize') {
        const shape = anchor.shapes[annotationDrag.selection.index];
        shape.width = Math.max(.01, Math.min(1-shape.x, point.x-shape.x));
        shape.height = Math.max(.01, Math.min(1-shape.y, point.y-shape.y));
      } else {
        anchor.shapes[annotationDrag.selection.index] = translateShape(anchor.shapes[annotationDrag.selection.index], dx, dy);
      }
      renderShapeElement(annotationDrag.selection.element, anchor.shapes[annotationDrag.selection.index]);
      updateAnnotationResizeHandle();
      if (annotationDrag.selection.kind === 'draft') annotationDraft.anchor = anchor;
      annotationDrag.after = anchor;
      annotationDrag.moved = Math.abs(dx) + Math.abs(dy) > .001;
      event.preventDefault();
      return;
    }
    if (!drawing || !drawing.preview) return;
    const box = drawing.overlay.getBoundingClientRect();
    const point = {x:Math.max(0,Math.min(1,(event.clientX-box.left)/box.width)),y:Math.max(0,Math.min(1,(event.clientY-box.top)/box.height))};
    if (drawing.mode === 'rect') {
      drawing.preview.setAttribute('x', Math.min(drawing.start.x,point.x)*1000);
      drawing.preview.setAttribute('y', Math.min(drawing.start.y,point.y)*1000);
      drawing.preview.setAttribute('width', Math.abs(point.x-drawing.start.x)*1000);
      drawing.preview.setAttribute('height', Math.abs(point.y-drawing.start.y)*1000);
      drawing.end = point;
    } else {
      drawing.points.push(point);
      drawing.preview.setAttribute('points', drawing.points.map(value => value.x*1000+','+value.y*1000).join(' '));
    }
  });

  document.addEventListener('pointerup', async () => {
    if (annotationDrag) {
      const drag = annotationDrag;
      annotationDrag = null;
      if (!drag.moved) return;
      if (drag.selection.kind === 'draft') {
        annotationDraft.undo.push(drag.before);
        annotationDraft.redo = [];
        q('[name=anchor]', q('.annotation-compose')).value = JSON.stringify(annotationDraft.anchor);
        updateHistoryControls();
      } else {
        try {
          await persistAnnotationAnchor(drag.selection.thread, drag.after);
        } catch (error) {
          renderShapeElement(drag.selection.element, drag.before.shapes[drag.selection.index]);
          alert('Could not move annotation: ' + error.message);
        }
      }
      return;
    }
    if (!drawing || !drawing.preview) return;
    const fragment = drawing.fragment;
    const preview = drawing.preview;
    let shape;
    if (drawing.mode === 'rect') {
      const point = drawing.end || drawing.start;
      shape = {type:'rect',x:Math.min(drawing.start.x,point.x),y:Math.min(drawing.start.y,point.y),width:Math.abs(point.x-drawing.start.x),height:Math.abs(point.y-drawing.start.y),color:drawing.color};
    } else {
      shape = {type:'path',points:drawing.points,color:drawing.color};
    }
    drawing.overlay.classList.remove('drawing');
    drawing = null;
    preview.remove();
    addDraftShape(fragment, shape);
  });

  updateHistoryControls();
  prepareLandmarks();
  qa('[data-text-target]').forEach(label => {
    const target = document.querySelector('[data-target="'+CSS.escape(label.dataset.textTarget)+'"] [data-selectable]');
    if (!target) return;
    const exact = label.dataset.exact;
    const color = normalizedAnnotationColor(label.dataset.textColor);
    const mark = markExactText(target, exact);
    if (mark) mark.style.backgroundColor = colorWithAlpha(color);
  });

  const firstFragment = q('.fragment');
  if (firstFragment) setActiveFragment(firstFragment);
  q('[data-file-filter]')?.addEventListener('input', filterTree);
  q('[data-hide-reviewed]')?.addEventListener('change', filterTree);
  q('[data-manifest-filter]')?.addEventListener('input', filterManifest);
  q('[data-annotation-color]')?.addEventListener('input', event => { annotationColor = normalizedAnnotationColor(event.target.value); });
  q('[data-annotation-color]')?.addEventListener('change', event => { if (selectedAnnotation) recolorSelectedAnnotation(event.target.value); });
  prepareContext();
  highlightCode();
  applyDiffLayout('inline');
  addEventListener('resize', () => { applyDiffLayout(diffLayout); positionLandmarkHotspots(); });
  const requestedView = new URL(location.href).searchParams.get('view');
  const initialView = requestedView === 'code' || requestedView === 'manifest' ? requestedView : 'saga';
  setView(initialView, false);
  setManifestMode('code');
  activateLandmark();
  addEventListener('hashchange', activateLandmark);
  addEventListener('popstate', () => {
    const view = new URL(location.href).searchParams.get('view');
    setView(view === 'code' || view === 'manifest' ? view : 'saga', false);
  });
})();`
