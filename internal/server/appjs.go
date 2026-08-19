package server

const appJavaScript = `(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => [...root.querySelectorAll(selector)];
  let active = null;

  function setView(name, updateURL = true) {
    qa('[data-view]').forEach(view => view.classList.toggle('active', view.dataset.view === name));
    qa('[data-view-tab]').forEach(tab => tab.classList.toggle('active', tab.dataset.viewTab === name));
    q('.saga-side').hidden = name !== 'saga';
    q('.code-side').hidden = name !== 'code';
    if (updateURL) {
      const url = new URL(location.href);
      if (name === 'code') url.searchParams.set('view', 'code'); else url.searchParams.delete('view');
      history.replaceState(null, '', url);
    }
  }

  function openDrawer(templateID) {
    const source = document.getElementById(templateID);
    if (!source) return;
    q('.drawer-body').innerHTML = source.innerHTML;
    q('.diff-drawer').classList.add('open');
    q('.diff-drawer').setAttribute('aria-hidden', 'false');
    q('.drawer-backdrop').classList.add('open');
    document.body.style.overflow = 'hidden';
  }

  function closeDrawer() {
    q('.diff-drawer').classList.remove('open');
    q('.diff-drawer').setAttribute('aria-hidden', 'true');
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

  function openComposer(fragment, anchor) {
    const form = q('.composer', fragment);
    form.classList.add('open');
    q('[name=anchor]', form).value = JSON.stringify(anchor);
    q('[name=body]', form).focus();
  }

  document.addEventListener('click', event => {
    const viewTab = event.target.closest('[data-view-tab]');
    if (viewTab) { setView(viewTab.dataset.viewTab); return; }
    const drawerButton = event.target.closest('[data-open-diffs]');
    if (drawerButton) { openDrawer(drawerButton.dataset.openDiffs); return; }
    if (event.target.closest('[data-close-drawer]')) { closeDrawer(); return; }
    const diffAction = event.target.closest('[data-diff-action]');
    if (diffAction) { openDiffComposer(diffAction); return; }
    if (event.target.closest('[data-close-diff-compose]')) { q('.diff-compose').classList.remove('open'); return; }

    const button = event.target.closest('[data-annotate]');
    if (!button) return;
    const fragment = button.closest('.fragment');
    const mode = button.dataset.annotate;
    if (mode === 'target') {
      openComposer(fragment, {type: 'target'});
      return;
    }
    if (mode === 'text') {
      const selection = getSelection();
      const selectable = q('[data-selectable]', fragment);
      if (!selection || selection.isCollapsed || !selectable.contains(selection.anchorNode) || !selectable.contains(selection.focusNode)) {
        alert('Select text in this fragment first.');
        return;
      }
      const exact = selection.toString();
      const selectedRange = selection.getRangeAt(0);
      const before = document.createRange();
      before.selectNodeContents(selectable);
      before.setEnd(selectedRange.startContainer, selectedRange.startOffset);
      const start = before.toString().length;
      const allText = selectable.textContent;
      openComposer(fragment, {type:'text',text:{exact,start,end:start+exact.length,prefix:allText.slice(Math.max(0,start-32),start),suffix:allText.slice(start+exact.length,start+exact.length+32)}});
      return;
    }
    const overlay = q('.review-overlay', fragment);
    overlay.classList.add('drawing');
    active = {fragment, overlay, mode, points: []};
  });

  document.addEventListener('keydown', event => {
    if (event.key !== 'Escape') return;
    closeDrawer();
    q('.diff-compose').classList.remove('open');
  });

  document.addEventListener('pointerdown', event => {
    if (!active || event.target !== active.overlay) return;
    const box = active.overlay.getBoundingClientRect();
    active.points = [{x:(event.clientX-box.left)/box.width,y:(event.clientY-box.top)/box.height}];
    active.start = active.points[0];
    active.preview = document.createElementNS('http://www.w3.org/2000/svg', active.mode === 'rect' ? 'rect' : 'polyline');
    active.preview.setAttribute('class', 'annotation pending ' + (active.mode === 'draw' ? 'path' : ''));
    active.overlay.append(active.preview);
    event.preventDefault();
  });

  document.addEventListener('pointermove', event => {
    if (!active || !active.preview) return;
    const box = active.overlay.getBoundingClientRect();
    const point = {x:Math.max(0,Math.min(1,(event.clientX-box.left)/box.width)),y:Math.max(0,Math.min(1,(event.clientY-box.top)/box.height))};
    if (active.mode === 'rect') {
      active.preview.setAttribute('x', Math.min(active.start.x,point.x)*1000);
      active.preview.setAttribute('y', Math.min(active.start.y,point.y)*1000);
      active.preview.setAttribute('width', Math.abs(point.x-active.start.x)*1000);
      active.preview.setAttribute('height', Math.abs(point.y-active.start.y)*1000);
      active.end = point;
    } else {
      active.points.push(point);
      active.preview.setAttribute('points', active.points.map(value => value.x*1000+','+value.y*1000).join(' '));
    }
  });

  document.addEventListener('pointerup', () => {
    if (!active || !active.preview) return;
    let shape;
    if (active.mode === 'rect') {
      const point = active.end || active.start;
      shape = {type:'rect',x:Math.min(active.start.x,point.x),y:Math.min(active.start.y,point.y),width:Math.abs(point.x-active.start.x),height:Math.abs(point.y-active.start.y)};
    } else {
      shape = {type:'path',points:active.points};
    }
    active.overlay.classList.remove('drawing');
    openComposer(active.fragment, {type:active.mode === 'rect' ? 'region' : 'drawing',coordinate_space:'normalized',shapes:[shape]});
    active = null;
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

  setView(new URL(location.href).searchParams.get('view') === 'code' ? 'code' : 'saga', false);
})();`
