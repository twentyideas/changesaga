package server

const appJavaScript = `(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => [...root.querySelectorAll(selector)];
  let drawing = null;
  let activeFragment = null;
  let selectedTool = 'select';

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
    if (event.key !== 'Escape') return;
    closeDrawer();
    closeAnnotation();
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
  const initialView = new URL(location.href).searchParams.get('view') === 'code' ? 'code' : 'saga';
  setView(initialView, false);
  addEventListener('popstate', () => setView(new URL(location.href).searchParams.get('view') === 'code' ? 'code' : 'saga', false));
})();`
