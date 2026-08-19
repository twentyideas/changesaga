package server

const appJavaScript = `(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => [...root.querySelectorAll(selector)];
  let drawing = null;
  let activeFragment = null;
  let selectedTool = 'select';
  let diffLayout = 'inline';
  let selectionAnchor = null;

  const languageKeywords = {
    go: new Set('break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var'.split(' ')),
    javascript: new Set('async await break case catch class const continue debugger default delete do else export extends finally for from function get if import in instanceof let new of return set static super switch this throw try typeof var void while with yield'.split(' ')),
    python: new Set('and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield'.split(' ')),
    ruby: new Set('alias and begin break case class def defined do else elsif end ensure false for if in module next nil not or redo rescue retry return self super then true undef unless until when while yield'.split(' ')),
    shell: new Set('case do done elif else esac fi for function if in select then time until while'.split(' ')),
    generic: new Set('class const enum false function interface let new null private public return static struct true type var void'.split(' '))
  };

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
      if (name === 'code') url.searchParams.set('view', 'code'); else url.searchParams.delete('view');
      history.pushState({view: name}, '', url);
    }
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
    q('.drawer-body').innerHTML = source.innerHTML;
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

  function openAnnotation(anchor) {
    if (!activeFragment) return;
    const form = q('.annotation-compose');
	form.reset();
    q('[name=target]', form).value = activeFragment.dataset.target;
    q('[name=anchor]', form).value = JSON.stringify(anchor);
    form.classList.add('open');
    q('[name=body]', form).focus();
    resetTool();
  }

  function closeAnnotation() {
    const form = q('.annotation-compose');
    if (form) form.classList.remove('open');
    resetTool();
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
      openAnnotation({type:'text',text:{exact,start,end:start+exact.length,prefix:allText.slice(Math.max(0,start-32),start),suffix:allText.slice(start+exact.length,start+exact.length+32)}});
      return;
    }
    const overlay = q('.review-overlay', activeFragment);
    if (!overlay) {
      resetTool();
      return;
    }
    overlay.classList.add('drawing');
    drawing = {fragment: activeFragment, overlay, mode, points: []};
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
    const viewTab = event.target.closest('[data-view-tab]');
    if (viewTab) { setView(viewTab.dataset.viewTab); return; }
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
    closeDrawer();
    closeAnnotation();
    updateLineSelection([]);
    const diffForm = q('.diff-compose');
    if (diffForm) diffForm.classList.remove('open');
    const dialog = q('.decision-dialog');
    if (dialog && dialog.open) dialog.close();
  });

  document.addEventListener('pointerdown', event => {
    if (!drawing || event.target !== drawing.overlay) return;
    const box = drawing.overlay.getBoundingClientRect();
    drawing.points = [{x:(event.clientX-box.left)/box.width,y:(event.clientY-box.top)/box.height}];
    drawing.start = drawing.points[0];
    drawing.preview = document.createElementNS('http://www.w3.org/2000/svg', drawing.mode === 'rect' ? 'rect' : 'polyline');
    drawing.preview.setAttribute('class', 'annotation pending ' + (drawing.mode === 'draw' ? 'path' : ''));
    drawing.overlay.append(drawing.preview);
    event.preventDefault();
  });

  document.addEventListener('pointermove', event => {
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

  document.addEventListener('pointerup', () => {
    if (!drawing || !drawing.preview) return;
    let shape;
    if (drawing.mode === 'rect') {
      const point = drawing.end || drawing.start;
      shape = {type:'rect',x:Math.min(drawing.start.x,point.x),y:Math.min(drawing.start.y,point.y),width:Math.abs(point.x-drawing.start.x),height:Math.abs(point.y-drawing.start.y)};
    } else {
      shape = {type:'path',points:drawing.points};
    }
    const anchor = {type:drawing.mode === 'rect' ? 'region' : 'drawing',coordinate_space:'normalized',shapes:[shape]};
    drawing.overlay.classList.remove('drawing');
    drawing = null;
    openAnnotation(anchor);
  });

  qa('[data-text-target]').forEach(label => {
    const target = document.querySelector('[data-target="'+CSS.escape(label.dataset.textTarget)+'"] [data-selectable]');
    if (!target) return;
    const exact = label.dataset.exact;
    const walker = document.createTreeWalker(target, NodeFilter.SHOW_TEXT);
    let node;
    while (node = walker.nextNode()) {
      const index = node.data.indexOf(exact);
      if (index >= 0) {
        const range = document.createRange();
        range.setStart(node,index); range.setEnd(node,index+exact.length);
        const mark = document.createElement('mark');
        range.surroundContents(mark);
        break;
      }
    }
  });

  const firstFragment = q('.fragment');
  if (firstFragment) setActiveFragment(firstFragment);
  q('[data-file-filter]')?.addEventListener('input', filterTree);
  q('[data-hide-reviewed]')?.addEventListener('change', filterTree);
  prepareContext();
  highlightCode();
  applyDiffLayout('inline');
  addEventListener('resize', () => applyDiffLayout(diffLayout));
  const initialView = new URL(location.href).searchParams.get('view') === 'code' ? 'code' : 'saga';
  setView(initialView, false);
  addEventListener('popstate', () => setView(new URL(location.href).searchParams.get('view') === 'code' ? 'code' : 'saga', false));
})();`
