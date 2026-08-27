package server

const appJavaScript = `(() => {
  const q = (selector, root = document) => root.querySelector(selector);
  const qa = (selector, root = document) => [...root.querySelectorAll(selector)];
  const mutationToken = q('meta[name="change-saga-mutation-token"]')?.content || '';
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
  let noteNudge = null;
  let annotationColorTouched = false;
  let drawerOpener = null;
  let drawerRestore = null;
  let pinnedBubble = null;
  let bubbleHideTimer = null;
  const noteDefaultColor = '#f2bd4b';

  const languageKeywords = {
    go: new Set('break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var'.split(' ')),
    javascript: new Set('async await break case catch class const continue debugger default delete do else export extends finally for from function get if import in instanceof let new of return set static super switch this throw try typeof var void while with yield'.split(' ')),
    python: new Set('and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield'.split(' ')),
    ruby: new Set('alias and begin break case class def defined do else elsif end ensure false for if in module next nil not or redo rescue retry return self super then true undef unless until when while yield'.split(' ')),
    shell: new Set('case do done elif else esac fi for function if in select then time until while'.split(' ')),
    generic: new Set('class const enum false function interface let new null private public return static struct true type var void'.split(' '))
  };

  function normalizedAnnotationColor(value, fallback = '#d04832') {
    return /^#[0-9a-f]{6}$/i.test(value || '') ? value.toLowerCase() : fallback;
  }

  function clampNormalized(value) {
    return Math.max(0, Math.min(1, Number(value) || 0));
  }

  function stickyNoteAnchor(text, x, y, color) {
    return {type:'note', coordinate_space:'normalized', note:{text, x:clampNormalized(x), y:clampNormalized(y), color:normalizedAnnotationColor(color, noteDefaultColor)}};
  }

  function translateNote(note, dx, dy) {
    return {...note, x:clampNormalized(note.x + dx), y:clampNormalized(note.y + dy)};
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
    const stepwise = shapeDraft || annotationDraft?.noteDraft;
    const redo = stepwise ? annotationDraft.redo.length && annotationDraft : annotationDraftRedo;
    // The buttons are icon-only, so the command name lives in the accessible
    // name and the tooltip rather than in replaceable text content.
    qa('[data-undo]').forEach(button => {
      button.disabled = !undo;
      const label = undo ? 'Undo ' + commandLabel(undo) : 'Nothing to undo';
      button.setAttribute('aria-label', label);
      button.title = (undo ? 'Undo ' + commandLabel(undo) : 'Undo') + ' (Ctrl/Cmd+Z)';
    });
    qa('[data-redo]').forEach(button => {
      button.disabled = !redo;
      const label = redo ? 'Redo ' + commandLabel(redo) : 'Nothing to redo';
      button.setAttribute('aria-label', label);
      button.title = (redo ? 'Redo ' + commandLabel(redo) : 'Redo') + ' (Ctrl+Y or Ctrl/Cmd+Shift+Z)';
    });
  }

  function submitReviewForm(action, fields, multipart = false) {
    const form = document.createElement('form');
    form.method = 'post';
    form.action = action;
    if (multipart) form.enctype = 'multipart/form-data';
    // form.submit() bypasses the submit listener, so the return path is explicit here.
    Object.entries({...fields, mutation_token:mutationToken, return_to:location.pathname + location.search + location.hash}).forEach(([name,value]) => {
      const input = document.createElement('input');
      input.type = 'hidden';
      input.name = name;
      input.value = value;
      form.append(input);
    });
    document.body.append(form);
    form.submit();
  }

  function submitThreadState(command, state) {
    submitReviewForm('/api/thread-state', {thread:command.thread, target:command.target, state});
  }

  function isReviewDecision(state) {
    return state === 'approved' || state === 'rejected';
  }

  function reviewDecisionStatus(state) {
    if (state === 'approved') return 'Approved';
    if (state === 'rejected') return 'Changes requested';
    return 'Not reviewed';
  }

  function directoryReviewState(state) {
    if (state === 'approved') return {state:'approved', status:'Approved'};
    if (state === 'rejected') return {state:'changes-requested', status:'Changes requested'};
    return {state:'unreviewed', status:'Unreviewed'};
  }

  function updateReviewDirectorySummary(directory) {
    const rows = qa('[data-review-directory-target]', directory);
    const decided = rows.filter(row => row.dataset.reviewState === 'approved' || row.dataset.reviewState === 'changes-requested').length;
    const summary = q('[data-review-directory-summary]', directory);
    if (summary) summary.textContent = decided + '/' + rows.length + ' reviewed';
  }

  function updateReviewDirectoryState(target, state) {
    const projected = directoryReviewState(state);
    qa('[data-review-directory-target]').filter(row => row.dataset.reviewDirectoryTarget === target).forEach(row => {
      row.dataset.reviewState = projected.state;
      row.classList.remove('unreviewed', 'approved', 'changes-requested');
      row.classList.add(projected.state);
      const status = q('[data-review-directory-status]', row);
      if (status) status.textContent = projected.status;
      updateReviewDirectorySummary(row.closest('[data-chapter-review-directory]'));
    });
  }

  let reviewProgressTimer = null;
  let reviewScrollTimer = null;

  function reviewProgressLabel(title, status, note = '') {
    return title + ': ' + status + (note ? '. Comment: ' + note : '');
  }

  function showReviewProgressTooltip(segment) {
    const progress = segment?.closest('[data-review-progress]');
    const tooltip = q('[data-review-progress-tooltip]', progress);
    if (!progress || !tooltip || !segment) return;
    const title = segment.dataset.reviewProgressTitle || 'Review item';
    const status = reviewDecisionStatus(segment.dataset.reviewState || '');
    const note = segment.dataset.reviewProgressNote || '';
    q('[data-review-progress-tooltip-title]', tooltip).textContent = title;
    q('[data-review-progress-tooltip-status]', tooltip).textContent = status;
    const noteElement = q('[data-review-progress-tooltip-note]', tooltip);
    noteElement.textContent = note;
    noteElement.hidden = !note;
    tooltip.hidden = false;
  }

  function hideReviewProgressTooltip(progress) {
    const tooltip = q('[data-review-progress-tooltip]', progress);
    if (tooltip) tooltip.hidden = true;
  }

  function updateReviewProgress(previous = '', next = '', emphasize = false, target = '', note = '') {
    const progress = q('[data-review-progress]');
    if (!progress) return;
    const total = Math.max(0, Number(document.body.dataset.reviewTotal) || 0);
    let decided = Math.max(0, Number(document.body.dataset.reviewDecided) || 0);
    const targetSegments = target ? qa('[data-review-progress-target]', progress).filter(segment => segment.dataset.reviewProgressTarget === target) : [];
    if ((previous || next) && (!target || targetSegments.length > 0)) {
      if (!isReviewDecision(previous) && isReviewDecision(next)) decided++;
      if (isReviewDecision(previous) && !isReviewDecision(next)) decided--;
      decided = Math.max(0, Math.min(total, decided));
      document.body.dataset.reviewDecided = String(decided);
    }
    progress.setAttribute('aria-label', 'Review progress: ' + decided + ' of ' + total + ' decisions');
    if (target) {
      targetSegments.forEach(segment => {
        const status = reviewDecisionStatus(next);
        const title = segment.dataset.reviewProgressTitle || 'Review item';
        segment.dataset.reviewState = next;
        segment.dataset.reviewProgressNote = note;
        segment.classList.remove('approved', 'rejected', 'pending');
        segment.classList.add(next === 'approved' || next === 'rejected' ? next : 'pending');
        segment.setAttribute('aria-label', reviewProgressLabel(title, status, note));
        segment.title = title + ' · ' + status + (note ? '\n' + note : '');
        if (segment.matches(':hover,:focus')) showReviewProgressTooltip(segment);
      });
    }
    if (!emphasize) return;
    progress.classList.add('changed');
    clearTimeout(reviewProgressTimer);
    reviewProgressTimer = setTimeout(() => progress.classList.remove('changed'), 1500);
  }

  function setReviewControlState(control, state, animate = true, note = '') {
    const previous = control.dataset.reviewState || '';
    const title = control.dataset.reviewTitle || 'item';
    const author = control.dataset.reviewAuthor || '';
    const attribution = control.dataset.reviewDetail || '';
    const matching = qa('[data-review-controls]').filter(candidate => candidate.dataset.reviewTarget === control.dataset.reviewTarget);
    matching.forEach(candidate => {
      candidate.dataset.reviewState = state;
      candidate.dataset.reviewAuthor = author;
      candidate.dataset.reviewDetail = attribution;
      const candidateTitle = candidate.dataset.reviewTitle || title;
      qa('[data-review-decision]', candidate).forEach(button => {
        const selected = button.dataset.reviewDecision === state;
        button.setAttribute('aria-pressed', String(selected));
        if (button.dataset.reviewDecision === 'approved') {
          button.setAttribute('aria-label', (selected ? 'Undo approval for ' : 'Approve ') + candidateTitle + (selected && author ? ' by ' + author : '') + (selected && note ? '. Comment: ' + note : ''));
          if (selected) button.removeAttribute('title'); else button.title = 'Approve';
        } else {
          button.setAttribute('aria-label', (selected ? 'Undo request for changes on ' : 'Request changes on ') + candidateTitle + (selected && author ? ' by ' + author : '') + (selected && note ? '. Comment: ' + note : ''));
          if (selected) button.removeAttribute('title'); else button.title = 'Request changes';
        }
        const tooltip = q('[data-review-decision-tooltip]', button);
        const tooltipAuthor = tooltip ? q('[data-review-decision-author]', tooltip) : null;
        const tooltipNote = tooltip ? q('[data-review-decision-tooltip-note]', tooltip) : null;
        if (tooltipAuthor) {
          tooltipAuthor.textContent = author;
          tooltipAuthor.title = attribution;
          tooltipAuthor.hidden = !author;
        }
        if (tooltipNote) {
          tooltipNote.textContent = note;
          tooltipNote.hidden = !note;
        }
      });
      const decisionNote = q('[data-review-note]', candidate);
      if (decisionNote) {
        decisionNote.textContent = note;
        decisionNote.title = note;
        decisionNote.hidden = !note;
      }
      if (!animate) return;
      candidate.classList.remove('decision-changed');
      requestAnimationFrame(() => candidate.classList.add('decision-changed'));
      setTimeout(() => candidate.classList.remove('decision-changed'), 650);
    });
    updateReviewDirectoryState(control.dataset.reviewTarget, state);
    updateReviewProgress(previous, state, animate, control.dataset.reviewTarget, note);
  }

  function closeReviewComposer(form, immediate = false) {
    if (!form) return;
    clearTimeout(form.reviewCloseTimer);
    form.classList.remove('open');
    const finish = () => {
      form.hidden = true;
      form.reset();
      form.reviewControl = null;
      document.body.append(form);
    };
    if (immediate) finish(); else form.reviewCloseTimer = setTimeout(finish, 150);
  }

  function openReviewComposer(control, state) {
    qa('[data-review-decision-form].open').forEach(form => closeReviewComposer(form, true));
    const form = q('[data-shared-review-form]');
    if (!form) return;
    clearTimeout(form.reviewCloseTimer);
    form.reset();
    form.reviewControl = control;
    control.append(form);
    q('[name=target]', form).value = control.dataset.reviewTarget;
    q('[name=state]', form).value = state;
    const field = q('[name=body]', form);
    field.placeholder = state === 'rejected' ? 'What needs to change? (optional)' : 'Why are you undoing this decision? (optional)';
    form.hidden = false;
    requestAnimationFrame(() => form.classList.add('open'));
    field.focus();
  }

  async function persistReviewDecision(control, state, body = '') {
    const controls = qa('[data-review-controls]').filter(candidate => candidate.dataset.reviewTarget === control.dataset.reviewTarget);
    const buttons = controls.flatMap(candidate => qa('button', candidate));
    buttons.forEach(button => button.disabled = true);
    try {
      const values = new URLSearchParams({target:control.dataset.reviewTarget, state, body, return_to:location.pathname + location.search + location.hash});
      const response = await fetch('/api/review', {method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded','X-Change-Saga-Async':'true','X-Change-Saga-Mutation-Token':mutationToken},body:values,credentials:'same-origin'});
      if (!response.ok) throw new Error((await response.text()).trim() || 'review could not be saved');
      controls.forEach(candidate => {
        candidate.dataset.reviewAuthor = 'Local / uncommitted';
        candidate.dataset.reviewDetail = 'This review event has not been committed yet.';
      });
      setReviewControlState(control, state, true, body.trim());
    } catch (error) {
      alert('Could not save this review decision: ' + error.message);
      throw error;
    } finally {
      buttons.forEach(button => button.disabled = false);
    }
  }

  function activateReviewDecision(button) {
    const control = button.closest('[data-review-controls]');
    const requested = button.dataset.reviewDecision;
    const current = control.dataset.reviewState || '';
    if (current === requested) {
      openReviewComposer(control, 'open');
      return;
    }
    if (requested === 'rejected') {
      openReviewComposer(control, 'rejected');
      return;
    }
    persistReviewDecision(control, 'approved').catch(() => {});
  }

  async function submitReviewComposer(form) {
    const control = form.reviewControl || form.closest('[data-review-controls]');
    if (!control) return;
    const state = q('[name=state]', form).value;
    const body = q('[name=body]', form).value;
    try {
      await persistReviewDecision(control, state, body);
      closeReviewComposer(form);
    } catch (_) {}
  }

  function openReviewComment(control) {
    discardAnnotationDraft();
    const form = q('.annotation-compose');
    form.reset();
    q('[name=target]', form).value = control.dataset.reviewTarget;
    q('[name=anchor]', form).value = JSON.stringify({type:'target'});
    q('.dialog-head h2', form).textContent = 'Comment on ' + (control.dataset.reviewTitle || 'this item');
    form.classList.add('open');
    positionAnnotationComposer(control);
    q('[name=body]', form).focus();
    resetTool();
    updateHistoryControls();
  }

  function resetAnnotationComposerPosition() {
    const form = q('.annotation-compose');
    if (!form) return;
    form.classList.remove('anchored');
    form.style.removeProperty('left');
    form.style.removeProperty('top');
  }

  function positionAnnotationComposer(anchor) {
    const form = q('.annotation-compose');
    const rect = anchor?.getBoundingClientRect ? anchor.getBoundingClientRect() : anchor;
    if (!form || !rect || !Number.isFinite(rect.left) || !Number.isFinite(rect.top)) {
      resetAnnotationComposerPosition();
      return;
    }
    form.classList.add('anchored');
    const width = form.offsetWidth;
    const height = form.offsetHeight;
    const gap = 10;
    const edge = 12;
    const clamp = (value, low, high) => Math.max(low, Math.min(Math.max(low, high), value));
    const centerLeft = rect.left + (rect.width - width) / 2;
    const centerTop = rect.top + (rect.height - height) / 2;
    const candidates = [
      {left:rect.right + gap, top:centerTop},
      {left:rect.left - width - gap, top:centerTop},
      {left:centerLeft, top:rect.bottom + gap},
      {left:centerLeft, top:rect.top - height - gap}
    ].map((candidate,index) => ({
      left:clamp(candidate.left, edge, innerWidth - width - edge),
      top:clamp(candidate.top, edge, innerHeight - height - edge),
      index
    }));
    const obstacles = [q('.annotation-toolbox:not([hidden])'), q('.topbar')]
      .filter(element => element?.getClientRects().length)
      .map(element => element.getBoundingClientRect());
    const overlap = (a,b) => Math.max(0, Math.min(a.right,b.right) - Math.max(a.left,b.left)) * Math.max(0, Math.min(a.bottom,b.bottom) - Math.max(a.top,b.top));
    const score = candidate => {
      const candidateRect = {left:candidate.left,top:candidate.top,right:candidate.left+width,bottom:candidate.top+height};
      const coveredControls = obstacles.reduce((area, obstacle) => area + overlap(candidateRect, obstacle), 0);
      const coveredAnchor = overlap(candidateRect, rect);
      const dx = Math.max(rect.left-candidateRect.right, candidateRect.left-rect.right, 0);
      const dy = Math.max(rect.top-candidateRect.bottom, candidateRect.top-rect.bottom, 0);
      return (coveredControls + coveredAnchor) * 1000 + Math.hypot(dx,dy) + candidate.index / 100;
    };
    candidates.sort((left,right) => score(left) - score(right));
    const {left,top} = candidates[0];
    form.style.left = Math.round(left + scrollX) + 'px';
    form.style.top = Math.round(top + scrollY) + 'px';
  }

  function shortcutDirection(event) {
    if (event.altKey || (!event.ctrlKey && !event.metaKey)) return '';
    const key = String(event.key || '').toLowerCase();
    if (key === 'z') return event.shiftKey ? 'redo' : 'undo';
    if (key === 'y' && event.ctrlKey && !event.shiftKey) return 'redo';
    return '';
  }

  function annotationDeleteShortcut(event) {
    if (event.ctrlKey || event.metaKey || event.altKey || event.key !== 'Delete' && event.key !== 'Backspace') return false;
    return !event.target.matches?.('input,textarea,[contenteditable="true"]');
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

  function normalizedMeasuredRegion(rect, rootRect) {
    if (!rect || !rootRect.width || !rootRect.height || (!rect.width && !rect.height)) return null;
    // A thin path or small glyph should still be easy to discover with a
    // pointer. Explicit --hotspot geometry remains the escape hatch when the
    // author's desired interaction area differs from the rendered bounds.
    const minimumX = 24 / rootRect.width;
    const minimumY = 24 / rootRect.height;
    const paddingX = 6 / rootRect.width;
    const paddingY = 6 / rootRect.height;
    const centerX = (rect.left + rect.right) / 2;
    const centerY = (rect.top + rect.bottom) / 2;
    const width = Math.min(1, Math.max(rect.width / rootRect.width + paddingX * 2, minimumX));
    const height = Math.min(1, Math.max(rect.height / rootRect.height + paddingY * 2, minimumY));
    const x = Math.max(0, Math.min(1 - width, (centerX - rootRect.left) / rootRect.width - width / 2));
    const y = Math.max(0, Math.min(1 - height, (centerY - rootRect.top) / rootRect.height - height / 2));
    return {x, y, width, height};
  }

  function appendAutomaticLandmarkHotspot(fragment, target, region) {
    const stage = q('.fragment-stage', fragment);
    if (!stage || q('[data-landmark-visual="' + CSS.escape(target.dataset.landmarkAnchor) + '"]', stage)) return;
    const visual = document.createElement('div');
    visual.className = 'landmark-hotspot';
    visual.dataset.landmarkVisual = target.dataset.landmarkAnchor;
    visual.dataset.autoLandmarkHotspot = 'true';
    visual.dataset.elementId = target.dataset.elementId;
    visual.dataset.x = String(region.x);
    visual.dataset.y = String(region.y);
    visual.dataset.width = String(region.width);
    visual.dataset.height = String(region.height);
    const affordance = cloneLandmarkAffordance(target);
    if (affordance) visual.append(affordance);
    stage.insertBefore(visual, q('.review-overlay', stage));
  }

  async function prepareSVGElementHotspots(fragment) {
    const frame = q('[data-fragment-frame]', fragment);
    const targets = qa('[data-landmark-target][data-landmark-type="element"]', fragment)
      .filter(target => target.dataset.elementId && !q('[data-landmark-visual="' + CSS.escape(target.dataset.landmarkAnchor) + '"]', fragment));
    if (!frame || targets.length === 0) return;
    const sourceURL = new URL(frame.getAttribute('src'), location.href);
    // SVG fragments created by the CLI have a .svg entrypoint. The aspect
    // query is also present for viewBox-based SVGs, including renamed assets.
    if (!sourceURL.pathname.toLowerCase().endsWith('.svg') && !sourceURL.searchParams.has('saga_aspect')) return;
    sourceURL.hash = '';
    const response = await fetch(sourceURL, {credentials:'same-origin'});
    if (!response.ok) return;
    const parsed = new DOMParser().parseFromString(await response.text(), 'image/svg+xml');
    if (parsed.querySelector('parsererror') || parsed.documentElement.localName !== 'svg') return;
    const svg = document.importNode(parsed.documentElement, true);
    // Measurement never needs executable or navigable content. Keeping this
    // clone inert preserves the iframe sandbox while still letting the browser
    // account for groups, paths, text, and transforms via normal SVG layout.
    svg.querySelectorAll('script,foreignObject').forEach(node => node.remove());
    svg.querySelectorAll('*').forEach(node => [...node.attributes].forEach(attribute => {
      const name = attribute.name.toLowerCase();
      if (name.startsWith('on') || ((name === 'href' || name.endsWith(':href')) && !attribute.value.startsWith('#'))) node.removeAttribute(attribute.name);
    }));
    const viewBox = svg.viewBox?.baseVal;
    if (!viewBox || !(viewBox.width > 0) || !(viewBox.height > 0)) return;
    const measure = document.createElement('div');
    measure.setAttribute('aria-hidden', 'true');
    measure.style.cssText = 'position:absolute;left:-100000px;top:0;visibility:hidden;pointer-events:none;overflow:hidden';
    svg.removeAttribute('width');
    svg.removeAttribute('height');
    svg.style.width = '1000px';
    svg.style.height = (1000 * viewBox.height / viewBox.width) + 'px';
    const shadow = measure.attachShadow({mode:'closed'});
    shadow.append(svg);
    document.body.append(measure);
    const rootRect = svg.getBoundingClientRect();
    targets.forEach(target => {
      const element = svg.querySelector('#' + CSS.escape(target.dataset.elementId));
      const region = normalizedMeasuredRegion(element?.getBoundingClientRect(), rootRect);
      if (region) appendAutomaticLandmarkHotspot(fragment, target, region);
    });
    measure.remove();
    positionLandmarkHotspots();
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

  // A comment drawn onto the content travels with its mark. The server places
  // each bubble from the stored anchor so it is already right without script;
  // here it is refined against the mark as the browser actually laid it out,
  // which is the only way to place a highlight at all.
  function annotationBubbleFor(threadID) {
    return threadID ? q('[data-annotation-bubble][data-thread-id="' + CSS.escape(threadID) + '"]') : null;
  }

  function annotationMarkFor(bubble) {
    const id = bubble?.dataset.threadId;
    if (!id) return null;
    const selector = '[data-thread-id="' + CSS.escape(id) + '"]';
    const stage = bubble.closest('.fragment-stage') || document;
    return q('[data-annotation-entity]' + selector, stage) || q('[data-sticky-note]' + selector, stage) || q('mark[data-text-mark]' + selector, stage);
  }

  function annotationBubbleAt(node) {
    const own = node?.closest?.('[data-annotation-bubble]');
    if (own) return own;
    const mark = node?.closest?.('[data-annotation-entity],[data-sticky-note],mark[data-text-mark]');
    return mark ? annotationBubbleFor(mark.dataset.threadId) : null;
  }

  function positionAnnotationBubbles(root = document) {
    let unplaced = 0;
    qa('[data-annotation-bubble]', root).forEach(bubble => {
      const stage = bubble.closest('.fragment-stage');
      const mark = annotationMarkFor(bubble);
      const box = mark?.getBoundingClientRect();
      const stageBox = stage?.getBoundingClientRect();
      if (!stageBox || !stageBox.width || !stageBox.height || !box || (!box.width && !box.height)) {
        // A mark the browser cannot measure yet — an unloaded image, a
        // highlight whose text moved — still needs a reachable comment, so the
        // bubble parks on the stage edge instead of vanishing.
        if (!bubble.classList.contains('placed')) {
          bubble.style.left = '100%';
          bubble.style.top = unplaced++ * 26 + 'px';
          bubble.classList.add('placed');
        }
        return;
      }
      bubble.style.left = clampNormalized((box.right - stageBox.left) / stageBox.width) * 100 + '%';
      bubble.style.top = clampNormalized((box.top - stageBox.top) / stageBox.height) * 100 + '%';
      bubble.classList.add('placed');
    });
  }

  function positionFragmentOverlays() {
    positionLandmarkHotspots();
    positionAnnotationBubbles();
  }

  // A revealed comment must stay on screen and must not sit on top of the mark
  // it describes. The panel is first nudged left far enough to fit in the
  // window, then moved above its bubble if that is what it takes to leave the
  // mark visible. A sliver of overlap beside a rectangle is not worth moving
  // for; burying a sticky note under its own comment is.
  function alignAnnotationBubblePanel(bubble) {
    const panel = q('[data-annotation-bubble-panel]', bubble);
    if (!panel) return;
    panel.classList.remove('flip-y');
    panel.style.marginLeft = '';
    const overflow = panel.getBoundingClientRect().right - (innerWidth - 8);
    if (overflow > 0) panel.style.marginLeft = -Math.min(overflow, Math.max(0, panel.getBoundingClientRect().left - 8)) + 'px';
    const box = panel.getBoundingClientRect();
    const mark = annotationMarkFor(bubble)?.getBoundingClientRect();
    const buriesMark = Boolean(mark)
      && Math.min(box.right, mark.right) - Math.max(box.left, mark.left) > 40
      && Math.min(box.bottom, mark.bottom) - Math.max(box.top, mark.top) > 10;
    const roomAbove = bubble.getBoundingClientRect().top - box.height - 13;
    if ((buriesMark || box.bottom > innerHeight - 8) && roomAbove > 0) panel.classList.add('flip-y');
  }

  function setAnnotationBubbleOpen(bubble, open) {
    if (!bubble) return;
    bubble.classList.toggle('open', open);
    const panel = q('[data-annotation-bubble-panel]', bubble);
    if (panel) panel.hidden = !open;
    q('[data-annotation-bubble-toggle]', bubble)?.setAttribute('aria-expanded', String(open));
    annotationMarkFor(bubble)?.classList.toggle('annotation-revealed', open);
    if (open) alignAnnotationBubblePanel(bubble);
  }

  function revealAnnotationBubble(bubble) {
    if (!bubble) return;
    clearTimeout(bubbleHideTimer);
    qa('[data-annotation-bubble].open').forEach(other => {
      if (other !== bubble && other !== pinnedBubble) setAnnotationBubbleOpen(other, false);
    });
    setAnnotationBubbleOpen(bubble, true);
  }

  // A bubble the reviewer is still using stays open: pinned by a click, holding
  // focus, or under the pointer on either the bubble or the mark it belongs to.
  function annotationBubbleHeld(bubble) {
    if (bubble === pinnedBubble || bubble.contains(document.activeElement)) return true;
    const mark = annotationMarkFor(bubble);
    return Boolean(bubble.matches?.(':hover') || mark?.matches?.(':hover'));
  }

  function hideAnnotationBubbleSoon(bubble) {
    if (!bubble) return;
    clearTimeout(bubbleHideTimer);
    // The gap between a mark and its bubble is real screen distance; closing
    // immediately would make the thread impossible to reach with the pointer.
    bubbleHideTimer = setTimeout(() => {
      if (!annotationBubbleHeld(bubble)) setAnnotationBubbleOpen(bubble, false);
    }, 180);
  }

  function pinAnnotationBubble(bubble) {
    if (pinnedBubble === bubble) {
      pinnedBubble = null;
      setAnnotationBubbleOpen(bubble, false);
      return;
    }
    const previous = pinnedBubble;
    pinnedBubble = bubble;
    if (previous) setAnnotationBubbleOpen(previous, false);
    revealAnnotationBubble(bubble);
  }

  function closeAnnotationBubbles() {
    pinnedBubble = null;
    clearTimeout(bubbleHideTimer);
    qa('[data-annotation-bubble].open').forEach(bubble => setAnnotationBubbleOpen(bubble, false));
  }

  function closeAnnotationBubble(bubble) {
    if (!bubble) return;
    if (pinnedBubble === bubble) pinnedBubble = null;
    const returnFocus = bubble.contains(document.activeElement);
    setAnnotationBubbleOpen(bubble, false);
    if (returnFocus) q('[data-annotation-bubble-toggle]', bubble)?.focus();
  }

  // A permalink to a comment must still open it now that the comment lives
  // inside a bubble rather than in the list under the content.
  function revealHashedAnnotationBubble() {
    const id = decodeURIComponent(location.hash.replace(/^#/, ''));
    const target = id ? document.getElementById(id) : null;
    const bubble = target?.closest?.('[data-annotation-bubble]');
    if (!bubble) return;
    pinnedBubble = bubble;
    revealAnnotationBubble(bubble);
    globalThis.requestAnimationFrame?.(() => target.scrollIntoView({block:'center'}));
  }

  // within lets a preparation pass run over one hydrated fragment as well as
  // over the whole page, including when the root is the fragment itself.
  function within(root, selector) {
    const scope = root || document;
    const self = scope.matches?.(selector) ? [scope] : [];
    return [...self, ...qa(selector, scope)];
  }

  function prepareLandmarks(root = document) {
    within(root, '.fragment-frame').forEach(frame => {
      const aspect = Number(new URL(frame.src, location.href).searchParams.get('saga_aspect'));
      if (aspect > 0) {
        frame.style.minHeight = '0';
        frame.style.aspectRatio = String(aspect);
      }
      frame.addEventListener('load', positionFragmentOverlays);
    });
    within(root, '.fragment-image').forEach(image => image.addEventListener('load', positionFragmentOverlays));
    within(root, '[data-landmark-target]').forEach(target => {
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
    within(root, '.fragment').forEach(fragment => { void prepareSVGElementHotspots(fragment).catch(() => {}); });
    globalThis.requestAnimationFrame?.(positionFragmentOverlays);
  }

  // Text highlights are drawn onto content, so they are applied once per piece
  // of content: over the page at load, and over each explanation as it arrives.
  function prepareTextHighlights(root = document) {
    within(root, '[data-text-target]').forEach(label => {
      const target = document.querySelector('[data-target="'+CSS.escape(label.dataset.textTarget)+'"] [data-selectable]');
      if (!target) return;
      const exact = label.dataset.exact;
      const color = normalizedAnnotationColor(label.dataset.textColor);
      const mark = markExactText(target, exact);
      if (!mark) return;
      mark.style.backgroundColor = colorWithAlpha(color);
      mark.dataset.textMark = 'true';
      mark.dataset.threadId = label.dataset.threadId || '';
    });
  }

  // Markdown citations are ordinary footnotes until their reference entry is
  // made into an exact-text landmark. When that landmark owns code evidence,
  // promote every inline citation marker into a direct diff-drawer control.
  // Footnotes without evidence keep their normal jump-to-reference behavior.
  function prepareDiffCitations(root = document) {
    within(root, 'a.footnote-ref').forEach(reference => {
      const href = reference.getAttribute('href') || '';
      if (!href.startsWith('#')) return;
      const definition = document.getElementById(decodeURIComponent(href.slice(1)));
      const diff = definition?.querySelector('[data-open-diffs]');
      if (!diff?.dataset.openDiffs) return;
      reference.dataset.openDiffs = diff.dataset.openDiffs;
      reference.classList.add('diff-citation');
      reference.setAttribute('aria-label', 'Open cited code');
      reference.setAttribute('title', 'Open cited code');
    });
  }

  async function activateLandmark() {
    qa('[data-landmark-visual].active').forEach(element => element.classList.remove('active'));
    qa('.content-landmark-active').forEach(element => element.classList.remove('content-landmark-active'));
    const id = decodeURIComponent(location.hash.replace(/^#/, ''));
    // The anchor may name something inside a chapter or an explanation that has
    // not been fetched yet, so it is resolved before it is scrolled to.
    const destination = await revealAnchor(id);
    if (destination?.closest('[data-view="saga"]')) {
      setView('saga', false);
    }
    const target = id ? q('[data-landmark-anchor="' + CSS.escape(id) + '"]') : null;
    if (!target) {
      destination?.scrollIntoView({block:'start'});
      return;
    }
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

  // Prose and licence files are not code. Running them through the tokeniser
  // painted every capitalised word as a type, which is exactly the decorative
  // noise a diff should not add to a sentence.
  const proseNames = new Set(['license','licence','notice','readme','contributing','changelog','authors','codeowners']);

  function languageForPath(path) {
    const name = (path || '').toLowerCase();
    const base = name.split('/').pop() || '';
    const extension = name.includes('.') ? name.split('.').pop() : '';
    if (['md','mdx','markdown','txt','text','rst','adoc'].includes(extension)) return 'prose';
    if (!name.includes('.') && proseNames.has(base)) return 'prose';
    if (extension === 'go') return 'go';
    if (['js','jsx','mjs','cjs','ts','tsx'].includes(extension)) return 'javascript';
    if (extension === 'py') return 'python';
    if (extension === 'rb') return 'ruby';
    if (['sh','bash','zsh'].includes(extension)) return 'shell';
    if (['json','yaml','yml','toml','xml','html','css','scss','sql','c','h','cc','cpp','java','rs','swift','kt'].includes(extension)) return extension;
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
      if (language === 'prose') { code.dataset.highlighted = language; return; }
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

  function prepareContext(root = document) {
    within(root, '[data-diff-body]').forEach(body => {
      qa(':scope > .context-expander', body).forEach(button => button.remove());
      qa(':scope > [data-context-row]', body).forEach(row => { row.hidden = false; });
      const rows = [...body.children].filter(row => row.matches('.diff-row'));
      const firstChange = rows.findIndex(row => !row.matches('[data-context-row]'));
      const lastChange = rows.findLastIndex(row => !row.matches('[data-context-row]'));
      const hidden = rows.map((row, index) => {
        if (!row.matches('[data-context-row]')) return false;
        let before = Infinity, after = Infinity;
        for (let i = index - 1; i >= 0; i--) if (!rows[i].matches('[data-context-row]')) { before = index - i; break; }
        for (let i = index + 1; i < rows.length; i++) if (!rows[i].matches('[data-context-row]')) { after = i - index; break; }
        return before > 3 && after > 3;
      });
      for (let index = 0; index < rows.length;) {
        if (!hidden[index]) { index++; continue; }
        const start = index;
        const group = [];
        while (index < rows.length && hidden[index]) { rows[index].hidden = true; group.push(rows[index]); index++; }
        installContextExpander(group, firstChange >= 0 && start > firstChange, lastChange >= 0 && index <= lastChange);
      }
    });
  }

  // GitHub-style context controls keep changed hunks visible while allowing a
  // reviewer to reveal unchanged lines from either edge of a collapsed gap.
  // The middle action opens the whole gap; directional actions reveal ten
  // lines at a time without discarding the reviewer's place in the diff.
  function installContextExpander(group, hasChangeBefore, hasChangeAfter) {
    let remaining = [...group];
    const step = 10;
    const control = document.createElement('div');
    control.className = 'context-expander';

    const action = (direction, glyph) => {
      const button = document.createElement('button');
      button.type = 'button';
      button.dataset.contextExpand = direction;
      button.textContent = glyph;
      button.addEventListener('click', () => reveal(direction));
      control.append(button);
      return button;
    };
    const down = hasChangeBefore ? action('down', '↓') : null;
    const all = action('all', '');
    all.className = 'context-expand-all';
    const up = hasChangeAfter ? action('up', '↑') : null;

    const refresh = () => {
      if (!remaining.length) {
        control.remove();
        return;
      }
      const count = remaining.length;
      const amount = Math.min(step, count);
      all.textContent = 'Expand all ' + count + ' unchanged lines';
      all.title = 'Expand the collapsed gap';
      if (down) {
        const label = 'Show next ' + amount + ' unchanged lines';
        down.setAttribute('aria-label', label);
        down.title = label;
      }
      if (up) {
        const label = 'Show previous ' + amount + ' unchanged lines';
        up.setAttribute('aria-label', label);
        up.title = label;
      }
      if (control.nextElementSibling !== remaining[0]) remaining[0].before(control);
    };
    const reveal = direction => {
      const acted = document.activeElement;
      const revealed = direction === 'all' ? remaining
        : direction === 'up' ? remaining.slice(-step)
        : remaining.slice(0, step);
      const revealedSet = new Set(revealed);
      revealed.forEach(row => { row.hidden = false; });
      remaining = remaining.filter(row => !revealedSet.has(row));
      refresh();
      applyDiffLayout(diffLayout);
      if (acted instanceof HTMLElement && [down, all, up].includes(acted)) {
        globalThis.requestAnimationFrame?.(() => {
          if (acted.isConnected) {
            acted.focus({preventScroll:true});
            return;
          }
          const towardPrevious = direction === 'down' || direction === 'all' && !hasChangeAfter;
          const edge = towardPrevious ? revealed[0] : revealed[revealed.length - 1];
          let neighbor = towardPrevious ? edge?.previousElementSibling : edge?.nextElementSibling;
          while (neighbor && !neighbor.matches('[data-diff-row]')) {
            neighbor = towardPrevious ? neighbor.previousElementSibling : neighbor.nextElementSibling;
          }
          neighbor?.querySelector('button,[href],[tabindex]')?.focus({preventScroll:true});
        });
      }
    };
    refresh();
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
    // Scoped to the toolbar buttons: the diff surface also carries data-layout,
    // and aria-pressed is not a valid attribute on that container.
    qa('button[data-layout]').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.layout === diffLayout)));
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

  function toggleDocNode(button) {
    const children = document.getElementById(button.getAttribute('aria-controls'));
    if (!children) return;
    const expanded = button.getAttribute('aria-expanded') === 'true';
    button.setAttribute('aria-expanded', String(!expanded));
    children.hidden = expanded;
  }

  // Opening a chapter is what fetches it. The disclosure state is applied at
  // once so the control never feels unresponsive, and the body arrives from
  // /api/section behind the placeholder the shell rendered in its place.
  async function setChapterOpen(chapter, open) {
    if (!chapter) return;
    const body = q('[data-chapter-body]', chapter);
    const toggle = q('[data-chapter-toggle]', chapter);
    if (!body || !toggle) return;
    body.hidden = !open;
    toggle.setAttribute('aria-expanded', String(open));
    toggle.setAttribute('aria-label', (open ? 'Close ' : 'Open ') + (q('.chapter-head h2', chapter)?.textContent.trim() || 'chapter'));
    chapter.classList.toggle('open', open);
    if (!open) return;
    await hydrateChapter(chapter);
    positionLandmarkHotspots();
  }

  function toggleChapter(button) {
	const chapter = button.closest('[data-chapter]');
	void setChapterOpen(chapter, button.getAttribute('aria-expanded') !== 'true');
  }

  // Code Diff and Coverage are deliberately absent from the root document.
  // Their endpoints return bounded HTML fragments, and a cold comparison may
  // answer 202 while its snapshot is still being built. Per-surface request
  // generations keep a late response for one file from replacing a newer deep
  // link, while the URL remains the source of truth throughout retries.
  const reviewSurfaceRequests = new Map();
  const reviewSurfaceRetries = new Map();
  const reviewFileRequests = new WeakMap();
  const continuousCoverageLoads = new WeakMap();
  let relatedOwnersRequest = null;

  function reviewSurfaceURL(name, explicitHref = '') {
    const surface = q('[data-review-surface="'+name+'"]');
    const url = new URL(explicitHref || surface?.dataset.surfaceHref || '', location.href);
    if (!explicitHref) {
      const current = new URL(location.href);
      ['file', 'diff', 'mode'].forEach(key => {
        if (current.searchParams.has(key)) url.searchParams.set(key, current.searchParams.get(key));
      });
    }
    return url;
  }

  function surfaceStatus(surface, state, title, detail, retry = false) {
    if (!surface) return;
    surface.dataset.surfaceState = state;
    surface.replaceChildren();
    const status = document.createElement('div');
    status.className = 'surface-placeholder ' + state;
    status.dataset.surfaceStatus = '';
    status.setAttribute('role', state === 'error' ? 'alert' : 'status');
    status.setAttribute('aria-live', 'polite');
    if (state === 'loading' || state === 'building') {
      const spinner = document.createElement('span');
      spinner.className = 'surface-spinner';
      spinner.setAttribute('aria-hidden', 'true');
      status.append(spinner);
    }
    const heading = document.createElement('strong');
    heading.textContent = title;
    status.append(heading);
    if (detail) {
      const message = document.createElement('span');
      message.textContent = detail;
      status.append(message);
    }
    if (retry) {
      const button = document.createElement('button');
      button.type = 'button';
      button.className = 'btn-primary';
      button.dataset.retrySurface = surface.dataset.reviewSurface;
      button.textContent = 'Try again';
      status.append(button);
    }
    surface.append(status);
  }

  function retryDelay(response) {
    const value = response.headers.get('Retry-After');
    const seconds = Number(value);
    if (Number.isFinite(seconds) && seconds >= 0) return Math.min(10_000, Math.max(250, seconds * 1000));
    const date = Date.parse(value || '');
    if (Number.isFinite(date)) return Math.min(10_000, Math.max(250, date - Date.now()));
    return 1000;
  }

  function prepareReviewSurface(name, root) {
    if (name !== 'manifest') {
      highlightCode(root);
      prepareContext(root);
      applyDiffLayout(diffLayout);
    }
    if (name === 'manifest') {
      const requested = new URL(location.href).searchParams.get('mode');
      const current = q('[data-manifest-mode][aria-pressed="true"]')?.dataset.manifestMode;
      setManifestMode(requested === 'saga' && q('[data-manifest-panel="saga"]') ? 'saga'
        : requested === 'code' && q('[data-manifest-panel="code"]') ? 'code'
        : current && q('[data-manifest-panel="'+current+'"]') ? current
        : q('[data-manifest-panel="code"]') ? 'code' : 'saga');
    }
    if (name === 'code') {
      const meta = q('[data-code-meta]');
      const content = q('[data-code-meta-content]', root);
      if (meta && content) meta.textContent = content.textContent;
      within(root, '[data-file-diff-href]').forEach(file => { void hydrateReviewFile(file); });
      void hydrateRelatedOwners(root);
    }
    const id = decodeURIComponent(location.hash.replace(/^#/, ''));
    const destination = id ? document.getElementById(id) : null;
    if (destination?.closest('[data-review-surface="'+name+'"]')) {
      revealHashedAnnotationBubble();
      globalThis.requestAnimationFrame?.(() => destination.scrollIntoView({block:'center'}));
    }
  }

  async function hydrateRelatedOwners(root) {
    const panel = q('#related-saga-panel', root);
    const file = q('[data-file-diff-href]', root);
    const filePath = file?.dataset.filePath;
    if (!panel || !filePath) return;
    const key = filePath;
    if (panel.dataset.relatedOwnersLoaded === key) return;
    relatedOwnersRequest?.controller.abort();
    const controller = new AbortController();
    const request = {key, controller};
    relatedOwnersRequest = request;
    try {
      const response = await fetch('/api/file-owners?file=' + encodeURIComponent(filePath), {
        headers:{Accept:'text/html','X-Change-Saga-Async':'true'}, credentials:'same-origin', signal:controller.signal
      });
      if (!response.ok) throw new Error('explanations request failed');
      const content = q('[data-file-owners-response]', parseShellHTML(await response.text()));
      if (!content) throw new Error('explanations response was incomplete');
      if (relatedOwnersRequest !== request || !panel.isConnected || q('[data-file-diff-href]', root)?.dataset.filePath !== filePath) return;
      panel.replaceChildren(...Array.from(content.childNodes));
      panel.dataset.relatedOwnersLoaded = key;
    } catch (error) {
      if (error.name !== 'AbortError' && panel.isConnected) panel.innerHTML = '<p>Explanations could not be loaded.</p>';
    } finally {
      if (relatedOwnersRequest === request) relatedOwnersRequest = null;
    }
  }

  // Code Diff and narrative-linked code are the same review surface. Both use
  // the same markup, row endpoint, cache, and bounded-page stream; only the
  // surrounding navigation differs. Context is prepared after the final page
  // arrives so a collapsed unchanged gap can span an endpoint boundary.
  function reviewFileIsActive(file) {
    return Boolean(file?.isConnected && (!file.matches('details') || file.open));
  }

  function cancelReviewFile(file) {
    reviewFileRequests.get(file)?.controller.abort();
  }

  async function hydrateReviewFile(file, options = {}) {
    const href = file?.dataset.fileDiffHref;
    const destination = q('[data-file-diff-rows]', file);
    const status = q('[data-file-diff-status]', file);
    if (!href || !destination) return file || null;
    const key = new URL(href, location.href).toString();
    const previous = reviewFileRequests.get(file);
    if (!options.force && previous?.key === key && !previous.controller.signal.aborted) return previous.promise;
    if (!options.force && file.dataset.fileDiffLoaded === key) return file;
    previous?.controller.abort();
    const controller = new AbortController();
    delete file.dataset.fileDiffLoaded;
    file.dataset.fileDiffLoading = 'true';
    q('[data-diff-surface]', file)?.classList.add('loading');
    const linkedContext = file.matches('.attached-file');
    if (status) status.textContent = linkedContext ? 'Loading full file diff; linked changes will be highlighted…' : 'Loading every changed hunk…';
    const visited = new Set();
    const promise = (async () => {
      let nextHref = href;
      let first = true;
      let loaded = 0;
      let total = 0;
      while (nextHref && reviewFileIsActive(file)) {
        const pageHref = new URL(nextHref, location.href).toString();
        if (visited.has(pageHref) || visited.size >= 10_000) throw new Error('diff cursor did not advance');
        visited.add(pageHref);
        const result = await fetchFileDiff(nextHref, {signal:controller.signal});
        if (!reviewFileIsActive(file)) {
          controller.abort();
          return file;
        }
        const wrapper = parseShellHTML(result.html);
        const page = q('[data-file-diff-page]', wrapper) || wrapper;
        const items = q('[data-page-items="lines"]', page) || page;
        const rows = qa('.diff-row', items);
        if (first) destination.replaceChildren(...rows); else destination.append(...rows);
        first = false;
        loaded += rows.length;
        total = result.total || total;
        if (status) status.textContent = total > 0
          ? 'Loaded ' + loaded + ' of ' + total + ' diff lines…'
          : 'Loaded ' + loaded + ' diff lines…';
        highlightCode(destination);
        nextHref = continuedPageURL(href, result.next || page.dataset?.nextCursor);
        if (nextHref) await new Promise(resolve => setTimeout(resolve, 0));
      }
      if (!reviewFileIsActive(file)) return file;
      file.dataset.fileDiffLoaded = key;
      if (status) status.textContent = linkedContext ? 'All changed hunks · linked changes highlighted' : 'All changed hunks';
      prepareContext(destination);
      applyDiffLayout(diffLayout);
      revealHashedAnnotationBubble();
      if (!location.hash) {
        const selected = q('.diff-row.selected', destination);
        globalThis.requestAnimationFrame?.(() => selected?.scrollIntoView({block:'center'}));
      }
      return file;
    })().catch(error => {
      if (error.name === 'AbortError') return file;
      visited.forEach(pageHref => fileDiffCache.delete(pageHref));
      destination.innerHTML = '<p class="diff-placeholder">This file could not be loaded. <button type="button" data-retry-file>Try again</button></p>';
      if (status) status.textContent = 'Could not load every changed hunk';
      return file;
    }).finally(() => {
      delete file.dataset.fileDiffLoading;
      q('[data-diff-surface]', file)?.classList.remove('loading');
      if (reviewFileRequests.get(file)?.promise === promise) reviewFileRequests.delete(file);
    });
    reviewFileRequests.set(file, {key, controller, promise});
    return promise;
  }

  function surfaceNextButton(name, cursor, pageKey = '') {
    if (!cursor) return null;
    const url = reviewSurfaceURL(name);
    url.searchParams.set('cursor', cursor);
    const button = document.createElement('button');
    button.type = 'button';
    button.dataset.surfaceNext = url.pathname + url.search;
    if (pageKey) button.dataset.pageTarget = pageKey;
    button.textContent = name === 'manifest' ? 'Loading more coverage…' : 'Load more';
    button.setAttribute('aria-label', name === 'manifest' ? 'Loading more coverage' : 'Load the next page');
    return button;
  }

  async function streamCoveragePages(surface, token) {
    let button = q('[data-surface-next]', surface);
    while (button && button.isConnected && continuousCoverageLoads.get(surface) === token) {
      button.disabled = true;
      button.setAttribute('aria-busy', 'true');
      const next = await loadReviewSurfacePage(button);
      if (!next) break;
      button = next;
      // Yield after every append so the browser paints useful coverage while
      // the following page is in flight instead of presenting one huge swap.
      await new Promise(resolve => setTimeout(resolve, 0));
    }
  }

  function beginContinuousCoverageLoad(surface) {
    if (!surface) return;
    const token = {};
    continuousCoverageLoads.set(surface, token);
    void streamCoveragePages(surface, token);
  }

  function installReviewSurface(name, surface, html, headerCursor = '') {
    const wrapper = parseShellHTML(html);
    const response = q('[data-review-surface-response="'+name+'"]', wrapper) || q('[data-view="'+name+'"]', wrapper);
    if (!response) throw new Error('surface response was incomplete');
    const next = surfaceNextButton(name, headerCursor || response.dataset.nextCursor, response.dataset.pageKey || (name === 'code' ? 'files' : ''));
    if (name === 'code') {
      const sidebarResponse = q('[data-code-sidebar-content]', response);
      const panelResponse = q('[data-code-panel-content]', response) || response;
      const sidebar = q('[data-code-sidebar]');
      if (!sidebarResponse || !sidebar) throw new Error('code response was incomplete');
      within(surface, '[data-file-diff-href]').forEach(cancelReviewFile);
      sidebar.replaceChildren(...Array.from(sidebarResponse.childNodes));
      surface.replaceChildren(...Array.from(panelResponse.childNodes));
    } else {
      surface.replaceChildren(...Array.from(response.childNodes));
    }
    if (next) surface.append(next);
    if (response.dataset.returned) surface.dataset.returned = response.dataset.returned;
    surface.dataset.surfaceState = 'ready';
    prepareReviewSurface(name, surface);
    if (name === 'manifest') beginContinuousCoverageLoad(surface);
  }

  async function hydrateReviewSurface(name, options = {}) {
    const surface = q('[data-review-surface="'+name+'"]');
    if (!surface) return null;
    const url = reviewSurfaceURL(name, options.href || '');
    const requestKey = url.toString();
    if (!options.force && surface.dataset.surfaceLoaded === requestKey) return surface;
    const previous = reviewSurfaceRequests.get(name);
    if (!options.force && previous?.key === requestKey) return previous.promise;
    previous?.controller.abort();
    clearTimeout(reviewSurfaceRetries.get(name));
    const controller = new AbortController();
    surfaceStatus(surface, 'loading', name === 'code' ? 'Loading Code Diff…' : 'Loading Coverage…', name === 'code' ? 'Loading changed files.' : 'Loading files and explanations.');
    const promise = fetch(url, {headers:{Accept:'text/html','X-Change-Saga-Async':'true'},credentials:'same-origin',signal:controller.signal}).then(async response => {
      if (response.status === 202) {
        const delay = retryDelay(response);
        surfaceStatus(surface, 'building', 'Building the source comparison…', 'This view will update automatically when it is ready.', true);
        const timer = setTimeout(() => {
          if (q('[data-view="'+name+'"]').classList.contains('active')) void hydrateReviewSurface(name, {force:true});
        }, delay);
        reviewSurfaceRetries.set(name, timer);
        return surface;
      }
      if (!response.ok) throw new Error((await response.text()).trim() || 'request failed');
      installReviewSurface(name, surface, await response.text(), response.headers.get('X-Change-Saga-Next-Cursor') || '');
      surface.dataset.surfaceLoaded = requestKey;
      return surface;
    }).catch(error => {
      if (error.name === 'AbortError') return surface;
      surfaceStatus(surface, 'error', name === 'code' ? 'Code Diff could not be loaded.' : 'Coverage could not be loaded.', error.message, true);
      return surface;
    }).finally(() => {
      if (reviewSurfaceRequests.get(name)?.promise === promise) reviewSurfaceRequests.delete(name);
    });
    reviewSurfaceRequests.set(name, {key:requestKey, controller, promise});
    return promise;
  }

  async function loadReviewSurfacePage(button) {
    const surface = button.closest('[data-review-surface]');
    const name = surface?.dataset.reviewSurface;
    const href = button.dataset.surfaceNext || button.getAttribute('href');
    if (!surface || !name || !href || button.dataset.pageLoading === 'true') return null;
    button.dataset.pageLoading = 'true';
    button.setAttribute('aria-busy', 'true');
    try {
      const response = await fetch(reviewSurfaceURL(name, href), {headers:{Accept:'text/html','X-Change-Saga-Async':'true'},credentials:'same-origin'});
      if (!response.ok) throw new Error('page request failed');
      if (!button.isConnected || button.closest('[data-review-surface]') !== surface) return null;
      const wrapper = parseShellHTML(await response.text());
      const page = q('[data-review-surface-page]', wrapper) || q('[data-review-surface-response="'+name+'"]', wrapper) || wrapper;
      const key = page.dataset?.pageKey || button.dataset.pageTarget || '';
      const groups = within(page, '[data-page-items]');
      let appended = 0;
      for (const items of groups) {
        const groupKey = items.dataset.pageItems;
        if (!groupKey) continue;
        const sidebar = q('[data-code-sidebar]');
        const destination = q('[data-page-items="'+CSS.escape(groupKey)+'"]', surface) || (sidebar ? q('[data-page-items="'+CSS.escape(groupKey)+'"]', sidebar) : null);
        if (!destination || destination === items) continue;
        destination.append(...Array.from(items.childNodes));
        appended++;
      }
      if (!appended) {
        const destinationRoot = name === 'code' && key === 'files' ? q('[data-code-sidebar]') : surface;
        const destination = key ? q('[data-page-items="'+CSS.escape(key)+'"]', destinationRoot) : q('[data-page-items]', destinationRoot);
        const items = key ? q('[data-page-items="'+CSS.escape(key)+'"]', page) : q('[data-page-items]', page);
        if (!destination || !items) throw new Error('page response was incomplete');
        destination.append(...Array.from(items.childNodes));
      }
      const next = q('[data-surface-next]', page) || surfaceNextButton(name, response.headers.get('X-Change-Saga-Next-Cursor') || page.dataset?.nextCursor, key);
      if (next) button.replaceWith(next); else button.remove();
      prepareReviewSurface(name, surface);
      return next;
    } catch (_) {
      button.textContent = name === 'manifest' ? 'Coverage paused — try again' : 'Could not load more — try again';
      delete button.dataset.pageLoading;
      button.removeAttribute('aria-busy');
      button.disabled = false;
      button.setAttribute('aria-label', name === 'manifest' ? 'Resume loading coverage' : 'Load the next page');
      return null;
    }
  }

  function setView(name, updateURL = true) {
    if (!q('[data-view="'+name+'"]')) name = 'saga';
    qa('[data-view]').forEach(view => view.classList.toggle('active', view.dataset.view === name));
    qa('[data-view-tab]').forEach(tab => {
      const selected = tab.dataset.viewTab === name;
      tab.classList.toggle('active', selected);
      tab.setAttribute('aria-selected', String(selected));
      // Roving focus: only the selected tab is in the sequential tab order.
      tab.tabIndex = selected ? 0 : -1;
    });
    const sagaSide = q('.saga-side');
    const codeSide = q('.code-side');
    const toolbox = q('.annotation-toolbox');
    const codeMeta = q('.top-meta');
    if (sagaSide) sagaSide.hidden = name !== 'saga';
    if (codeSide) codeSide.hidden = name !== 'code';
    if (toolbox) toolbox.hidden = name !== 'saga' || !toolbox.dataset.annotationTarget;
    if (codeMeta) codeMeta.hidden = name !== 'code';
    const shell = q('[data-shell]');
    if (shell) shell.classList.toggle('code-mode', name === 'code');
    // A hidden view measures as zero, so the bubbles are placed once the saga
    // view is actually on screen.
    if (name === 'saga') globalThis.requestAnimationFrame?.(positionFragmentOverlays);
    if (updateURL) {
      const url = new URL(location.href);
      if (name === 'saga') url.searchParams.delete('view'); else url.searchParams.set('view', name);
      history.pushState({view: name}, '', url);
    }
    if (name === 'code' || name === 'manifest') void hydrateReviewSurface(name);
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
      // Folders are structure, not matches: hide the ones whose files all went
      // away so the tree never leaves an empty branch behind.
      qa('[data-manifest-folder]', panel).reverse().forEach(folder => {
        folder.hidden = !q('[data-manifest-search]:not([hidden])', folder);
        if (query && !folder.hidden) folder.open = true;
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

  function activateManifestMode(mode) {
    const current = new URL(location.href);
    current.searchParams.set('mode', mode);
    history.pushState({view:'manifest', mode}, '', current);
    if (q('[data-manifest-panel="'+mode+'"]')) {
      setManifestMode(mode);
      return Promise.resolve();
    }
    const url = reviewSurfaceURL('manifest');
    url.searchParams.set('mode', mode);
    return hydrateReviewSurface('manifest', {force:true, href:url.toString()});
  }

  function setActiveFragment(fragment) {
    if (!fragment || fragment === activeFragment) return;
    if (activeFragment) activeFragment.classList.remove('active-fragment');
    activeFragment = fragment;
    activeFragment.classList.add('active-fragment');
    // Pointing at an explanation is the clearest signal that it is about to be
    // read or annotated, so it is fetched now rather than when it scrolls.
    if (fragment.dataset.fragmentHref) void hydrateFragment(fragment);
  }

  function hideAnnotationTools() {
    const toolbox = q('[data-annotation-target]');
    if (!toolbox) return;
    toolbox.hidden = true;
    toolbox.dataset.annotationTarget = '';
    toolbox.setAttribute('aria-label', 'Annotation tools');
    qa('[data-annotation-tools]').forEach(button => {
      button.setAttribute('aria-expanded', 'false');
      button.setAttribute('aria-label', 'Show annotation tools for ' + (button.dataset.annotationTitle || 'this explanation'));
    });
    document.body.append(toolbox);
    resetTool();
  }

  function showAnnotationTools(fragment, target = fragment?.dataset.target || '', title = fragment?.dataset.fragmentTitle || 'this explanation') {
    const toolbox = q('[data-annotation-target]');
    const actions = fragment ? q(':scope > .fragment-head > .fragment-actions', fragment) : null;
    if (!fragment || !toolbox || !actions) return false;
    setActiveFragment(fragment);
    actions.append(toolbox);
    toolbox.dataset.annotationTarget = target;
    toolbox.setAttribute('aria-label', 'Annotation tools for ' + title);
    const label = q('[data-tool-target]', toolbox);
    if (label) label.textContent = title;
    toolbox.hidden = false;
    qa('[data-annotation-tools]').forEach(candidate => {
      const selected = candidate.dataset.annotationTools === target;
      candidate.setAttribute('aria-expanded', String(selected));
      candidate.setAttribute('aria-label', (selected ? 'Hide' : 'Show') + ' annotation tools for ' + (candidate.dataset.annotationTitle || 'this explanation'));
    });
    resetTool();
    return true;
  }

  async function toggleAnnotationTools(button) {
    const fragment = button.closest('.fragment');
    const toolbox = q('[data-annotation-target]');
    if (!fragment || !toolbox) return;
    closeAnnotationBubbles();
    const target = button.dataset.annotationTools || fragment.dataset.target || '';
    if (!toolbox.hidden && toolbox.dataset.annotationTarget === target) {
      hideAnnotationTools();
      return;
    }
    if (annotationDraft || drawing) closeAnnotation();
    if (fragment.dataset.fragmentHref) await hydrateFragment(fragment);
    if (fragment.dataset.fragmentHref) return;
    const title = fragment.dataset.fragmentTitle || button.dataset.annotationTitle || 'this explanation';
    showAnnotationTools(fragment, target, title);
  }

  function cancelDrawing() {
    if (!drawing) return;
    drawing.overlay.classList.remove('drawing', 'placing');
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

  function configureDrawer(mode, title) {
    const drawer = q('.diff-drawer');
    if (!drawer) return;
    drawer.dataset.drawerMode = mode;
    drawer.setAttribute('aria-label', mode === 'fragment' ? 'Related explanation' : 'Linked code');
    const heading = q('.drawer-head strong', drawer);
    if (heading) heading.textContent = title;
    const icon = q('.drawer-head .i use', drawer);
    if (icon) icon.setAttribute('href', mode === 'fragment' ? '#i-book' : '#i-diff');
    const close = q('[data-close-drawer]', drawer);
    if (close) {
      close.setAttribute('aria-label', mode === 'fragment' ? 'Close related explanation' : 'Close linked code');
      close.title = 'Close';
    }
  }

  function restoreDrawerContent() {
    const body = q('.drawer-body');
    if (body) within(body, '[data-file-diff-href]').forEach(cancelReviewFile);
    if (drawerRestore) {
      if (drawerRestore.placeholder.isConnected) drawerRestore.placeholder.replaceWith(drawerRestore.fragment);
      drawerRestore = null;
    }
    if (body) body.replaceChildren();
  }

  function showDrawer(opener) {
    drawerOpener = opener instanceof HTMLElement && opener.isConnected
      ? opener
      : (document.activeElement instanceof HTMLElement ? document.activeElement : null);
    const drawer = q('.diff-drawer');
    drawer.classList.add('open');
    drawer.removeAttribute('inert');
    drawer.setAttribute('aria-hidden', 'false');
    q('.drawer-backdrop').classList.add('open');
    document.body.style.overflow = 'hidden';
    q('[data-close-drawer]', drawer).focus();
  }

  function openDrawer(templateID, opener) {
    const lazy = qa('[data-target-code-template]').find(candidate => candidate.dataset.targetCodeTemplate === templateID);
    if (lazy) { void hydrateTargetCode(lazy); return; }
    const source = document.getElementById(templateID);
    if (!source) return;
    // WebKit does not consistently move document.activeElement to a button
    // before dispatching its click event. Preserve the event's explicit opener
    // so Escape always restores focus to the control the reviewer activated.
    const returnOpener = drawerRestore ? drawerOpener : opener;
    restoreDrawerContent();
    const body = q('.drawer-body');
    body.innerHTML = source.innerHTML;
    const attached = q('[data-attached-title]', body);
    configureDrawer('code', attached?.dataset.attachedTitle ? 'Linked code · ' + attached.dataset.attachedTitle : 'Linked code');
    highlightCode(body);
    showDrawer(returnOpener);
  }

  // One changed file's diff body is fetched the first time a reviewer opens it
  // and then reused for the rest of the session. The page deliberately ships no
  // diff bodies: a large comparison would otherwise appear in the document once
  // per narrative target that explains it and again in the coverage audit, as
  // markup that stays inside a closed disclosure until it is asked for.
  const fileDiffCache = new Map();

  async function fetchFileDiff(href, options = {}) {
    const key = new URL(href, location.href).toString();
    const cached = fileDiffCache.get(key);
    if (cached) return cached;
    const signal = options.signal || new AbortController().signal;
    while (true) {
      const response = await fetch(href, {headers:{Accept:'text/html'}, signal});
      if (response.status === 202) {
        await abortableDelay(retryDelay(response), signal);
        continue;
      }
      if (!response.ok) throw new Error('diff request failed');
      const result = {
        html:await response.text(),
        next:response.headers.get('X-Change-Saga-Next-Cursor') || '',
        total:Number(response.headers.get('X-Change-Saga-Total')) || 0,
        returned:Number(response.headers.get('X-Change-Saga-Returned')) || 0
      };
      fileDiffCache.set(key, result);
      return result;
    }
  }

  // The page ships the saga as a shell: saga identity, coverage totals, the
  // overview's explanations as descriptors, one summary per chapter, and the
  // navigation outline. Chapter bodies and explanation content arrive from
  // /api/section and /api/fragment as a reviewer reaches them, so first load
  // stays proportional to what is on screen rather than to the whole story.
  const shellCache = new Map();

  function fetchShell(href) {
    let request = shellCache.get(href);
    if (!request) {
      const load = () => fetch(href, {headers:{Accept:'text/html'}}).then(async response => {
        if (response.status === 202) {
          await new Promise(resolve => setTimeout(resolve, retryDelay(response)));
          return load();
        }
        if (!response.ok) throw new Error('shell request failed');
        return response.text();
      });
      request = load();
      shellCache.set(href, request);
      request.catch(() => shellCache.delete(href));
    }
    return request;
  }

  function parseShellHTML(html) {
    const wrapper = document.createElement('div');
    wrapper.innerHTML = html;
    return wrapper;
  }

  // Linked-code summaries are small enough to anticipate but expensive enough
  // that a pointer sweep must not fan out across the story. Two speculative
  // requests run at once; clicks promote their request above hover work, and a
  // bounded LRU keeps completed summaries useful without retaining the saga.
  const maxConcurrentTargetCodeLoads = 2;
  const targetCodeCacheLimit = 64;
  const targetCodeResponses = new Map();
  const targetCodeJobs = new Map();
  const targetCodeQueue = [];
  let activeTargetCodeLoads = 0;
  let targetCodeJobOrder = 0;

  function cachedTargetCode(href) {
    const html = targetCodeResponses.get(href);
    if (html === undefined) return null;
    targetCodeResponses.delete(href);
    targetCodeResponses.set(href, html);
    return html;
  }

  function rememberTargetCode(href, html) {
    targetCodeResponses.delete(href);
    targetCodeResponses.set(href, html);
    while (targetCodeResponses.size > targetCodeCacheLimit) {
      targetCodeResponses.delete(targetCodeResponses.keys().next().value);
    }
  }

  function requestAbortError() {
    return new DOMException('Request was cancelled', 'AbortError');
  }

  function abortableDelay(milliseconds, signal) {
    return new Promise((resolve, reject) => {
      if (signal.aborted) { reject(requestAbortError()); return; }
      const timer = setTimeout(resolve, milliseconds);
      signal.addEventListener('abort', () => { clearTimeout(timer); reject(requestAbortError()); }, {once:true});
    });
  }

  async function loadTargetCodeJob(job) {
    while (true) {
      const response = await fetch(job.href, {headers:{Accept:'text/html'}, credentials:'same-origin', signal:job.controller.signal});
      if (response.status === 202) {
        await abortableDelay(retryDelay(response), job.controller.signal);
        continue;
      }
      if (!response.ok) throw new Error('linked-code request failed');
      return response.text();
    }
  }

  function pumpTargetCodeQueue() {
    targetCodeQueue.sort((left, right) => right.priority-left.priority || left.order-right.order);
    while (activeTargetCodeLoads < maxConcurrentTargetCodeLoads && targetCodeQueue.length) {
      const job = targetCodeQueue.shift();
      if (job.state !== 'queued') continue;
      job.state = 'running';
      job.controller = new AbortController();
      activeTargetCodeLoads++;
      void loadTargetCodeJob(job).then(html => {
        if (job.state === 'cancelled') throw requestAbortError();
        rememberTargetCode(job.href, html);
        installTargetCodeResponse(job.href, html);
        job.resolve(html);
      }).catch(error => job.reject(error)).finally(() => {
        if (targetCodeJobs.get(job.href) === job) targetCodeJobs.delete(job.href);
        activeTargetCodeLoads--;
        pumpTargetCodeQueue();
      });
    }
  }

  function preemptForInteractiveTargetCode(interactiveJob) {
    if (interactiveJob.state !== 'queued' || activeTargetCodeLoads < maxConcurrentTargetCodeLoads) return;
    const victim = Array.from(targetCodeJobs.values())
      .filter(job => job !== interactiveJob && job.state === 'running' && !job.interactive)
      .sort((left, right) => left.priority-right.priority || right.order-left.order)[0];
    victim?.controller.abort();
  }

  function requestTargetCode(href, options = {}) {
    const cached = cachedTargetCode(href);
    if (cached !== null) return Promise.resolve(cached);
    let job = targetCodeJobs.get(href);
    if (!job) {
      let resolve, reject;
      const promise = new Promise((accept, decline) => { resolve = accept; reject = decline; });
      job = {href, promise, resolve, reject, state:'queued', controller:null, scopes:new Set(), interactive:false, priority:0, order:targetCodeJobOrder++};
      targetCodeJobs.set(href, job);
      targetCodeQueue.push(job);
    }
    if (options.scope) job.scopes.add(options.scope);
    if (options.interactive) job.interactive = true;
    job.priority = Math.max(job.priority, options.interactive ? 100 : Number(options.priority || 0));
    preemptForInteractiveTargetCode(job);
    pumpTargetCodeQueue();
    return job.promise;
  }

  function cancelTargetCodeScope(scope) {
    targetCodeJobs.forEach(job => {
      if (!job.scopes.delete(scope) || job.interactive || job.scopes.size) return;
      if (job.state === 'running') {
        job.state = 'cancelled';
        targetCodeJobs.delete(job.href);
        job.controller.abort();
      } else if (job.state === 'queued') {
        job.state = 'cancelled';
        targetCodeJobs.delete(job.href);
        job.reject(requestAbortError());
      }
    });
  }

  const fragmentPrefetchScopes = new WeakMap();

  function fragmentPrefetchScope(fragment) {
    let scope = fragmentPrefetchScopes.get(fragment);
    if (!scope) {
      scope = {fragment, reasons:new Set(), timer:null, started:false};
      fragmentPrefetchScopes.set(fragment, scope);
    }
    return scope;
  }

  function fragmentTargetCodeHrefs(fragment) {
    const direct = q(':scope > .fragment-head [data-target-code-href]', fragment)?.dataset.targetCodeHref || '';
    const seen = new Set();
    const hrefs = [];
    const add = href => { if (href && !seen.has(href)) { seen.add(href); hrefs.push(href); } };
    add(direct);
    within(fragment, '[data-target-code-href]').forEach(control => add(control.dataset.targetCodeHref));
    return {direct, hrefs};
  }

  function beginFragmentPrefetch(fragment, reason) {
    if (!fragment) return;
    const scope = fragmentPrefetchScope(fragment);
    scope.reasons.add(reason);
    if (scope.timer || scope.started) return;
    scope.timer = setTimeout(async () => {
      scope.timer = null;
      if (!scope.reasons.size) return;
      scope.started = true;
      if (fragment.dataset.fragmentHref) await hydrateFragment(fragment);
      if (!scope.reasons.size || !fragment.isConnected) { scope.started = false; return; }
      const targets = fragmentTargetCodeHrefs(fragment);
      targets.hrefs.forEach(href => {
        void requestTargetCode(href, {scope, priority:href === targets.direct ? 10 : 1}).catch(() => {});
      });
    }, 160);
  }

  function endFragmentPrefetch(fragment, reason) {
    const scope = fragmentPrefetchScopes.get(fragment);
    if (!scope) return;
    scope.reasons.delete(reason);
    if (scope.reasons.size) return;
    clearTimeout(scope.timer);
    scope.timer = null;
    scope.started = false;
    cancelTargetCodeScope(scope);
  }

  function installTargetCodeResponse(href, html, preferredButton = null) {
    const response = q('[data-target-code-response]', parseShellHTML(html));
    if (!response) throw new Error('linked-code response was incomplete');
    const readyButton = q('[data-open-diffs]', response);
    const readyTemplate = q('template', response);
    const controls = qa('[data-target-code-href]').filter(candidate => candidate.dataset.targetCodeHref === href);
    const templateID = readyTemplate?.id || controls.find(candidate => candidate.dataset.targetCodeTemplate)?.dataset.targetCodeTemplate || '';
    const staleButtons = templateID ? qa('button[data-open-diffs]').filter(candidate =>
      candidate.dataset.openDiffs === templateID && candidate.getAttribute('aria-label') === 'Open related code') : [];
    const landmarkMarker = ':landmark:';
    const landmarkAt = (response.dataset.targetCodeTarget || '').lastIndexOf(landmarkMarker);
    if (landmarkAt >= 0) {
      const fragmentTarget = response.dataset.targetCodeTarget.slice(0, landmarkAt);
      const landmarkID = response.dataset.targetCodeTarget.slice(landmarkAt + landmarkMarker.length);
      const fragment = qa('.fragment').find(candidate => candidate.dataset.target === fragmentTarget);
      if (fragment) within(fragment, '.landmark-list > div').forEach(row => {
        const anchor = q('a[href^="#"]', row)?.getAttribute('href')?.slice(1) || '';
        const button = q('button[data-open-diffs]', row);
        if (button && anchor.endsWith('--' + landmarkID) && !staleButtons.includes(button)) staleButtons.push(button);
      });
    }
    if (!readyButton || !readyTemplate) {
      controls.forEach(candidate => candidate.remove());
      staleButtons.forEach(candidate => candidate.remove());
      prepareDiffCitations();
      return null;
    }
    const existingTemplate = document.getElementById(readyTemplate.id);
    if (existingTemplate) existingTemplate.replaceWith(readyTemplate);
    else document.body.append(readyTemplate);
    let opener = null;
    controls.forEach(candidate => {
      const replacement = readyButton.cloneNode(true);
      if (candidate === preferredButton) opener = replacement;
      candidate.replaceWith(replacement);
    });
    staleButtons.forEach(candidate => {
      const replacement = readyButton.cloneNode(true);
      if (candidate === preferredButton) opener = replacement;
      candidate.replaceWith(replacement);
    });
    prepareDiffCitations();
    opener ||= qa('button[data-open-diffs]').find(candidate => candidate.dataset.openDiffs === readyButton.dataset.openDiffs) || null;
    return {templateID:readyButton.dataset.openDiffs, opener};
  }

  async function hydrateTargetCode(button) {
    const href = button?.dataset.targetCodeHref;
    if (!href || button.dataset.targetCodeLoading === 'true') return;
    button.dataset.targetCodeLoading = 'true';
    button.setAttribute('aria-busy', 'true');
    try {
      const installed = installTargetCodeResponse(href, await requestTargetCode(href, {interactive:true}), button);
      if (installed) openDrawer(installed.templateID, installed.opener);
    } catch (_) {
      delete button.dataset.targetCodeLoading;
      button.removeAttribute('aria-busy');
      button.title = 'Linked code could not be loaded — try again';
    }
  }

  function installAuxiliaryDiffNext(container, href, cursor) {
    q('[data-aux-file-next]', container)?.remove();
    if (!cursor) return;
    const url = new URL(href, location.href);
    url.searchParams.set('cursor', cursor);
    const button = document.createElement('button');
    button.type = 'button';
    button.dataset.auxFileNext = url.pathname + url.search;
    button.textContent = 'Load more lines';
    button.setAttribute('aria-label', 'Load the next file chunk');
    container.append(button);
  }

  async function appendAuxiliaryDiff(button) {
    const container = button.parentElement;
    const href = button.dataset.auxFileNext;
    if (!container || !href || button.dataset.loading === 'true') return;
    button.dataset.loading = 'true';
    try {
      const result = await fetchFileDiff(href);
      const wrapper = parseShellHTML(result.html);
      const items = q('[data-page-items="lines"]', wrapper);
      const rows = items ? qa('.diff-row', items) : [];
      button.before(...rows);
      installAuxiliaryDiffNext(container, href, result.next);
      highlightCode(container);
      prepareContext(container);
      applyDiffLayout(diffLayout);
    } catch (_) {
      button.textContent = 'Could not load more — try again';
      delete button.dataset.loading;
    }
  }

  async function hydrateChapter(chapter) {
    const href = chapter?.dataset.sectionHref;
    const body = chapter ? q('[data-chapter-body]', chapter) : null;
    if (!href || !body) return;
    if (chapter.dataset.sectionLoading === 'true') { await fetchShell(href).catch(() => {}); return; }
    chapter.dataset.sectionLoading = 'true';
    try {
      const wrapper = parseShellHTML(await fetchShell(href));
      body.replaceChildren(...Array.from(wrapper.childNodes));
      delete chapter.dataset.sectionHref;
      observeDeferredFragments(body);
    } catch (_) {
      const placeholder = q('[data-section-placeholder]', body);
      if (placeholder) placeholder.textContent = 'This chapter could not be loaded. Close and reopen to try again.';
    } finally {
      delete chapter.dataset.sectionLoading;
    }
  }

  async function hydrateFragment(article) {
    const href = article?.dataset.fragmentHref;
    if (!href) return article || null;
    if (article.dataset.fragmentLoading === 'true') { await fetchShell(href).catch(() => {}); return article; }
    article.dataset.fragmentLoading = 'true';
    try {
      const replacement = q('.fragment', parseShellHTML(await fetchShell(href)));
      if (!replacement) throw new Error('explanation response was incomplete');
      // A decision the reviewer has already made is not undone by content
      // arriving after it: the live controls move into the rendered explanation
      // instead of being replaced by the state its snapshot was built from.
      const live = q(':scope > .fragment-head [data-review-controls]', article);
      const rendered = q(':scope > .fragment-head [data-review-controls]', replacement);
      if (live && rendered) rendered.replaceWith(live);
      // The article itself is never swapped out. A reviewer can be part way
      // through clicking a descriptor's controls when its content arrives, and
      // replacing the element under the pointer loses that click: the detached
      // node no longer reaches the document that handles it. Filling the article
      // in place keeps its head where it was and every live control attached,
      // and keeps this explanation the active one without re-selecting it.
      for (const attribute of Array.from(replacement.attributes)) article.setAttribute(attribute.name, attribute.value);
      article.removeAttribute('data-fragment-href');
      delete article.dataset.fragmentLoading;
      article.replaceChildren(...Array.from(replacement.childNodes));
      prepareLandmarks(article);
      prepareDiffCitations(article);
      prepareTextHighlights(article);
      highlightCode(article);
      positionFragmentOverlays();
      return article;
    } catch (_) {
      delete article.dataset.fragmentLoading;
      const placeholder = q('[data-fragment-placeholder]', article);
      if (placeholder) placeholder.textContent = 'This explanation could not be loaded. Reload the page to try again.';
      return article;
    }
  }

  // A fragment is fetched when it is close enough to be read. Chapter bodies
  // stay hidden until they are opened, so nothing inside a closed chapter is
  // observed and nothing inside it is fetched.
  const fragmentObserver = typeof IntersectionObserver === 'function' ? new IntersectionObserver(entries => {
    entries.forEach(entry => {
      if (!entry.isIntersecting) return;
      fragmentObserver.unobserve(entry.target);
      void hydrateFragment(entry.target);
    });
  }, {rootMargin:'400px'}) : null;

  // Returns when everything this call decided to fetch has arrived, so the page
  // can say when it has finished filling itself in.
  function observeDeferredFragments(root = document) {
    const arriving = [];
    within(root, '[data-fragment-href]').forEach(article => {
      if (!fragmentObserver) { arriving.push(hydrateFragment(article)); return; }
      // What is already on screen is fetched now rather than one frame later.
      // The observer's first callback costs a frame the reviewer would spend
      // looking at a placeholder, and reflowing under a pointer that has
      // already arrived is worse than fetching a little too eagerly.
      if (article.getBoundingClientRect().top <= innerHeight + 400) { arriving.push(hydrateFragment(article)); return; }
      fragmentObserver.observe(article);
    });
    return Promise.all(arriving);
  }

  // A permalink can name a heading, a marked place, or a comment inside a
  // chapter nobody has opened yet. The server answers where one anchor lives;
  // shipping the same answer as an index would put every anchor in the document
  // into every first load, which is the cost this shell exists to remove.
  const anchorPlaces = new Map();

  function locateAnchor(id) {
    let request = anchorPlaces.get(id);
    if (!request) {
      request = fetch('/api/locate?anchor=' + encodeURIComponent(id), {headers:{Accept:'application/json'}})
        .then(response => response.ok ? response.json() : null)
        .catch(() => null);
      anchorPlaces.set(id, request);
    }
    return request;
  }

  async function revealAnchor(id) {
    if (!id) return null;
    let element = document.getElementById(id);
    const pending = !element ||
      element.closest('[data-section-href]') !== null ||
      element.closest('[data-fragment-href]') !== null;
    if (pending) {
      const place = await locateAnchor(id);
      if (place?.chapter) await hydrateChapter(document.getElementById(place.chapter));
      if (place?.fragment) await hydrateFragment(document.getElementById(place.fragment));
      element = document.getElementById(id);
    }
    const chapter = element?.closest('[data-chapter]');
    if (chapter) await setChapterOpen(chapter, true);
    return document.getElementById(id);
  }

  // Coverage shows the same bodies to answer a different question, so it uses
  // the same per-file endpoint and the same cache. Only the disclosure that
  // owns a surface hydrates it, so opening one narrative target does not pull
  // in every file underneath it.
  async function hydrateManifestDiff(surface) {
    if (surface.dataset.manifestDiffLoaded === 'true' || surface.dataset.manifestDiffLoading === 'true') return;
    const href = surface.dataset.manifestDiffHref;
    const rows = q('[data-manifest-diff-rows]', surface);
    if (!href || !rows) return;
    surface.dataset.manifestDiffLoading = 'true';
    surface.classList.add('loading');
    try {
      const wrapper = document.createElement('div');
      const result = await fetchFileDiff(href);
      wrapper.innerHTML = result.html;
      rows.replaceChildren(...Array.from(wrapper.childNodes));
	  installAuxiliaryDiffNext(rows, href, result.next);
      surface.dataset.manifestDiffLoaded = 'true';
      highlightCode(rows);
    } catch (_) {
      const placeholder = q('[data-diff-placeholder]', rows);
      if (placeholder) placeholder.textContent = 'This file diff could not be loaded. Close and reopen to try again.';
    } finally {
      delete surface.dataset.manifestDiffLoading;
      surface.classList.remove('loading');
    }
  }

  function continuedPageURL(href, cursor) {
    if (!cursor) return '';
    const url = new URL(href, location.href);
    url.searchParams.set('cursor', cursor);
    return url.pathname + url.search;
  }

  async function hydrateCoverageFile(details) {
    if (!details?.open || details.dataset.coverageFileLoaded === 'true' || details.dataset.coverageFileLoading === 'true') return;
    let href = details.dataset.coverageFileHref;
    const destination = q('[data-coverage-file-mappings]', details);
    if (!href || !destination) return;
    details.dataset.coverageFileLoading = 'true';
    let first = true;
    try {
      while (href && details.isConnected) {
        const response = await fetch(href, {headers:{Accept:'text/html','X-Change-Saga-Async':'true'},credentials:'same-origin'});
        if (response.status === 202) {
          await new Promise(resolve => setTimeout(resolve, retryDelay(response)));
          continue;
        }
        if (!response.ok) throw new Error('file coverage request failed');
        const page = q('[data-coverage-file-response]', parseShellHTML(await response.text()));
        if (!page) throw new Error('file coverage response was incomplete');
        const items = q('[data-page-items="coverage-file"]', page);
        if (!items) throw new Error('file coverage response was incomplete');
        const inserted = Array.from(items.childNodes);
        if (first) destination.replaceChildren(...inserted); else destination.append(...inserted);
        first = false;
        href = continuedPageURL(href, response.headers.get('X-Change-Saga-Next-Cursor') || page.dataset.nextCursor);
        await new Promise(resolve => setTimeout(resolve, 0));
      }
      details.dataset.coverageFileLoaded = 'true';
      if (!destination.childNodes.length) destination.innerHTML = '<p class="diff-placeholder">This file is not explained yet.</p>';
    } catch (_) {
      const placeholder = q('.diff-placeholder', destination);
      if (placeholder) placeholder.textContent = 'Coverage details could not be loaded. Close and reopen to try again.';
    } finally {
      delete details.dataset.coverageFileLoading;
    }
  }

  async function hydrateCoverageTarget(details) {
    if (!details?.open || details.dataset.coverageTargetLoaded === 'true' || details.dataset.coverageTargetLoading === 'true') return;
    let href = details.dataset.coverageTargetHref;
    const destination = q('[data-coverage-target-files]', details);
    if (!href || !destination) return;
    details.dataset.coverageTargetLoading = 'true';
    let first = true;
    try {
      while (href && details.isConnected) {
        const response = await fetch(href, {headers:{Accept:'text/html','X-Change-Saga-Async':'true'},credentials:'same-origin'});
        if (response.status === 202) {
          await new Promise(resolve => setTimeout(resolve, retryDelay(response)));
          continue;
        }
        if (!response.ok) throw new Error('target coverage request failed');
        const page = q('[data-coverage-target-response]', parseShellHTML(await response.text()));
        if (!page) throw new Error('target coverage response was incomplete');
        const items = q('[data-page-items="target-files"]', page);
        if (!items) throw new Error('target coverage response was incomplete');
        const inserted = Array.from(items.childNodes);
        if (first) destination.replaceChildren(...inserted); else destination.append(...inserted);
        first = false;
        href = continuedPageURL(href, response.headers.get('X-Change-Saga-Next-Cursor') || page.dataset.nextCursor);
        filterManifest();
        await new Promise(resolve => setTimeout(resolve, 0));
      }
      details.dataset.coverageTargetLoaded = 'true';
      if (!destination.childNodes.length) destination.innerHTML = '<p class="diff-placeholder">This part of the story has no linked files.</p>';
    } catch (_) {
      const placeholder = q('.diff-placeholder', destination);
      if (placeholder) placeholder.textContent = 'Linked files could not be loaded. Close and reopen to try again.';
    } finally {
      delete details.dataset.coverageTargetLoading;
    }
  }

  function hydrateOpenedManifestDiffs(details) {
    if (!details?.open) return;
    qa('[data-manifest-diff-href]', details)
      .filter(surface => surface.closest('details') === details)
      .forEach(hydrateManifestDiff);
  }

  async function openFragmentDrawer(anchor, opener) {
	anchor = decodeURIComponent(String(anchor || '').replace(/^#/, ''));
    const destination = await revealAnchor(anchor);
    const fragment = destination?.matches('.fragment') ? destination : destination?.closest('.fragment');
    if (!fragment) return;
    restoreDrawerContent();
    const placeholder = document.createComment('change-saga fragment drawer');
    fragment.replaceWith(placeholder);
    drawerRestore = {fragment, placeholder};
    q('.drawer-body').append(fragment);
    configureDrawer('fragment', fragment.dataset.fragmentTitle || 'Related explanation');
    setActiveFragment(fragment);
    showDrawer(opener);
    positionFragmentOverlays();
    requestAnimationFrame(() => {
      const visual = q('[data-landmark-visual="' + CSS.escape(anchor) + '"]', fragment);
      (visual || destination).scrollIntoView({block:'center'});
    });
  }

  function closeDrawer() {
    const drawer = q('.diff-drawer');
    if (!drawer) return;
    const wasOpen = drawer.classList.contains('open');
    drawer.classList.remove('open');
    drawer.setAttribute('aria-hidden', 'true');
    // Return focus before the drawer becomes inert. WebKit does not always make
    // a clicked close button active, so do not condition this on activeElement
    // still being inside the drawer.
    if (wasOpen && drawerOpener?.isConnected) drawerOpener.focus();
    drawer.setAttribute('inert', '');
    q('.drawer-backdrop').classList.remove('open');
    document.body.style.overflow = '';
    restoreDrawerContent();
    drawerOpener = null;
    configureDrawer('code', 'Linked code');
  }

  // A diff row already carries the change it is: its exact diff URI, the
  // narrative target a comment on it belongs to, and its content. The per-line
  // buttons used to repeat all three, which cost more than the code itself in a
  // large file, so they now read the row they sit in.
  function diffActionContext(button) {
    const row = button.closest('[data-diff-ref]');
    return {dataset:{
      diffAction: button.dataset.diffAction,
      diffRef: button.dataset.diffRef || row?.dataset.diffRef,
      target: button.dataset.target || row?.dataset.target || '',
      content: button.dataset.content ?? (q('[data-code]', row)?.textContent || '')
    }};
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
    if (anchor?.type === 'note') return 'sticky note';
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

  function noteButton(label, attribute) {
    const button = document.createElement('button');
    button.type = 'button';
    button.textContent = label;
    button.setAttribute(attribute, 'true');
    return button;
  }

  function noteTextField(value) {
    const field = document.createElement('textarea');
    field.className = 'sticky-note-text';
    field.rows = 4;
    field.maxLength = 2000;
    field.placeholder = 'Write a note';
    field.setAttribute('aria-label', 'Sticky note text');
    field.value = value;
    return field;
  }

  function renderNoteElement(element, note) {
    const color = normalizedAnnotationColor(note.color, noteDefaultColor);
    element.dataset.x = String(note.x);
    element.dataset.y = String(note.y);
    element.dataset.color = color;
    element.style.setProperty('--note-color', color);
    element.style.left = note.x * 100 + '%';
    element.style.top = note.y * 100 + '%';
  }

  function createNoteElement(note, pending) {
    const element = document.createElement('div');
    element.className = 'sticky-note' + (pending ? ' pending' : '');
    element.dataset.stickyNote = 'true';
    element.tabIndex = 0;
    element.setAttribute('role', 'note');
    element.setAttribute('aria-label', pending ? 'New sticky note' : 'Sticky note');
    const body = document.createElement('p');
    body.className = 'sticky-note-body';
    body.dataset.stickyText = 'true';
    // Note text is only ever written as a text node, never as markup.
    body.textContent = note.text || '';
    const actions = document.createElement('span');
    actions.className = 'sticky-note-actions';
    element.append(body, actions);
    if (pending) {
      body.hidden = true;
      element.prepend(noteTextField(note.text || ''));
      actions.append(noteButton('Add note', 'data-commit-note'), noteButton('Cancel', 'data-cancel-note'));
    }
    renderNoteElement(element, note);
    return element;
  }

  function noteAnchorFromElement(element) {
    return stickyNoteAnchor(q('[data-sticky-text]', element)?.textContent || '', Number(element.dataset.x), Number(element.dataset.y), element.dataset.color);
  }

  function persistedAnchor(group) {
    return {type:group.dataset.anchorType,coordinate_space:'normalized',shapes:qa('[data-shape-index]', group).map(shapeFromElement)};
  }

  function clearAnnotationSelection() {
    q('.annotation.selected')?.classList.remove('selected');
    q('.sticky-note.selected')?.classList.remove('selected');
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
    const fragment = element.closest('.fragment');
    if (fragment) showAnnotationTools(fragment);
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
    element.setAttribute('tabindex', '-1');
    element.focus?.({preventScroll:true});
  }

  function selectStickyNote(element) {
    const fragment = element.closest('.fragment');
    if (fragment) showAnnotationTools(fragment);
    clearAnnotationSelection();
    selectedAnnotation = {
      kind:element.classList.contains('pending') ? 'draft' : 'persisted',
      note:true,
      element,
      thread:element.dataset.threadId || '',
      target:element.dataset.target || '',
      state:element.dataset.threadState || 'open'
    };
    element.classList.add('selected');
    const tools = q('[data-annotation-selection]');
    if (tools) tools.hidden = false;
    const color = normalizedAnnotationColor(element.dataset.color, noteDefaultColor);
    annotationColor = color;
    const picker = q('[data-annotation-color]');
    if (picker) picker.value = color;
    setSelectedTool('select');
    element.focus?.({preventScroll:true});
  }

  function mountNoteDraft(fragment, anchor) {
    const stage = q('.fragment-stage', fragment);
    if (!stage) return null;
    const element = createNoteElement(anchor.note, true);
    stage.append(element);
    const field = q('.sticky-note-text', element);
    field.addEventListener('input', () => {
      if (annotationDraft?.noteDraft) annotationDraft.anchor.note.text = field.value;
    });
    field.focus();
    return element;
  }

  function beginNoteDraft(fragment, point) {
    discardAnnotationDraft();
    setActiveFragment(fragment);
    const anchor = stickyNoteAnchor('', point.x, point.y, annotationColor);
    annotationDraft = {kind:'draft', noteDraft:true, target:fragment.dataset.target, fragment, anchor, undo:[], redo:[], label:'sticky note'};
    annotationDraft.element = mountNoteDraft(fragment, anchor);
    updateHistoryControls();
  }

  function restoreNoteDraft(draft) {
    discardAnnotationDraft();
    setActiveFragment(draft.fragment);
    annotationDraft = {...draft};
    annotationDraft.element = mountNoteDraft(draft.fragment, annotationDraft.anchor);
    updateHistoryControls();
  }

  function syncNoteDraft() {
    if (!annotationDraft?.noteDraft || !annotationDraft.element) return;
    renderNoteElement(annotationDraft.element, annotationDraft.anchor.note);
    const field = q('.sticky-note-text', annotationDraft.element);
    if (field) field.value = annotationDraft.anchor.note.text || '';
    clearAnnotationSelection();
    updateHistoryControls();
  }

  function discardNoteDraft() {
    if (!annotationDraft?.noteDraft) return;
    qa('.sticky-note.pending').forEach(element => element.remove());
    annotationDraftRedo = annotationDraft;
    annotationDraft = null;
    clearAnnotationSelection();
    resetTool();
    updateHistoryControls();
  }

  function commitNoteDraft() {
    const draft = annotationDraft;
    if (!draft?.noteDraft) return;
    const field = q('.sticky-note-text', draft.element);
    const text = (field?.value || '').trim();
    if (!text) {
      field?.focus();
      return;
    }
    const anchor = cloneAnchor(draft.anchor);
    anchor.note.text = text;
    submitReviewForm('/api/thread', {target:draft.target, kind:'comment', anchor:JSON.stringify(anchor), body:text}, true);
  }

  function beginNoteEdit(element) {
    if (q('.sticky-note-text', element)) return;
    const body = q('[data-sticky-text]', element);
    const field = noteTextField(body.textContent);
    body.hidden = true;
    element.prepend(field);
    q('.sticky-note-actions', element).prepend(noteButton('Save', 'data-save-note'), noteButton('Cancel', 'data-cancel-note'));
    field.focus();
    field.setSelectionRange(field.value.length, field.value.length);
  }

  async function endNoteEdit(element, commit) {
    const field = q('.sticky-note-text', element);
    if (!field) return;
    const body = q('[data-sticky-text]', element);
    const before = noteAnchorFromElement(element);
    const text = field.value.trim();
    field.remove();
    qa('[data-save-note],[data-cancel-note]', element).forEach(button => button.remove());
    body.hidden = false;
    element.focus?.({preventScroll:true});
    if (!commit || !text || text === before.note.text) return;
    const anchor = cloneAnchor(before);
    anchor.note.text = text;
    body.textContent = text;
    try {
      await persistAnnotationAnchor(element.dataset.threadId, anchor);
    } catch (error) {
      body.textContent = before.note.text;
      alert('Could not update the note: ' + error.message);
    }
  }

  function discardAnnotationDraft() {
    qa('.annotation.pending').forEach(element => element.remove());
    qa('.sticky-note.pending').forEach(element => element.remove());
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
    if (form.classList.contains('open')) {
      const marks = qa('.annotation.pending', overlay);
      positionAnnotationComposer(marks[marks.length - 1] || annotationDraft.fragment);
    }
    clearAnnotationSelection();
    updateHistoryControls();
  }

  function ensureShapeDraft(fragment) {
    if (annotationDraft?.shapeDraft && annotationDraft.fragment === fragment) return annotationDraft;
    const form = q('.annotation-compose');
    discardAnnotationDraft();
    form.reset();
    q('.dialog-head h2', form).textContent = 'Add a comment';
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
    q('.dialog-head h2', form).textContent = 'Add a comment';
    q('[name=target]', form).value = fragment.dataset.target;
    q('[name=anchor]', form).value = JSON.stringify(anchor);
    q('[name=body]', form).value = options.body || '';
    form.classList.add('open');
    positionAnnotationComposer(options.anchorElement || options.anchorRect || q('.fragment-head', fragment));
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
    resetAnnotationComposerPosition();
    if (discard) {
      discardAnnotationDraft();
    }
    resetTool();
    updateHistoryControls();
  }

  function undoDraft() {
    if (!annotationDraft) return false;
    if (annotationDraft.noteDraft) {
      if (stepShapeDraftHistory(annotationDraft, 'undo')) {
        syncNoteDraft();
        return true;
      }
      discardNoteDraft();
      return true;
    }
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
    if (annotationDraft?.noteDraft) {
      if (!stepShapeDraftHistory(annotationDraft, 'redo')) return false;
      syncNoteDraft();
      return true;
    }
    if (!annotationDraftRedo) return false;
    const draft = annotationDraftRedo;
    annotationDraftRedo = null;
    if (draft.noteDraft) {
      restoreNoteDraft(draft);
      return true;
    }
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
      headers:{'Content-Type':'application/x-www-form-urlencoded','X-Change-Saga-Mutation-Token':mutationToken},
      body:new URLSearchParams({thread,anchor:JSON.stringify(anchor)})
    });
    if (!response.ok) throw new Error(await response.text());
  }

  function selectedAnchor() {
    if (!selectedAnnotation) return null;
    if (selectedAnnotation.kind === 'draft') return cloneAnchor(annotationDraft.anchor);
    return selectedAnnotation.note ? noteAnchorFromElement(selectedAnnotation.element) : persistedAnchor(selectedAnnotation.group);
  }

  function nudgeSelectedNote(dx, dy) {
    if (!selectedAnnotation?.note) return;
    const anchor = selectedAnchor();
    if (!noteNudge) noteNudge = {selection:selectedAnnotation, before:cloneAnchor(anchor)};
    anchor.note = translateNote(anchor.note, dx, dy);
    renderNoteElement(selectedAnnotation.element, anchor.note);
    if (selectedAnnotation.kind === 'draft') annotationDraft.anchor = anchor;
    noteNudge.after = anchor;
    positionAnnotationBubbles(selectedAnnotation.element.closest('.fragment-stage') || document);
  }

  function commitNoteNudge() {
    if (!noteNudge) return;
    const nudge = noteNudge;
    noteNudge = null;
    if (!nudge.after) return;
    if (nudge.selection.kind === 'draft') {
      annotationDraft.undo.push(nudge.before);
      annotationDraft.redo = [];
      updateHistoryControls();
      return;
    }
    persistAnnotationAnchor(nudge.selection.thread, nudge.after).catch(error => {
      renderNoteElement(nudge.selection.element, nudge.before.note);
      alert('Could not move the note: ' + error.message);
    });
  }

  async function recolorSelectedAnnotation(color) {
    if (!selectedAnnotation) return;
    const before = selectedAnchor();
    const anchor = cloneAnchor(before);
    if (selectedAnnotation.note) {
      anchor.note.color = normalizedAnnotationColor(color, noteDefaultColor);
      renderNoteElement(selectedAnnotation.element, anchor.note);
    } else {
      anchor.shapes[selectedAnnotation.index].color = normalizedAnnotationColor(color);
      renderShapeElement(selectedAnnotation.element, anchor.shapes[selectedAnnotation.index]);
    }
    if (selectedAnnotation.kind === 'draft') {
      annotationDraft.undo.push(before);
      annotationDraft.redo = [];
      annotationDraft.anchor = anchor;
      if (!annotationDraft.noteDraft) q('[name=anchor]', q('.annotation-compose')).value = JSON.stringify(anchor);
      updateHistoryControls();
      return;
    }
    try {
      await persistAnnotationAnchor(selectedAnnotation.thread, anchor);
    } catch (error) {
      if (selectedAnnotation.note) renderNoteElement(selectedAnnotation.element, before.note);
      else renderShapeElement(selectedAnnotation.element, before.shapes[selectedAnnotation.index]);
      alert('Could not update annotation: ' + error.message);
    }
  }

  async function removeSelectedAnnotation() {
    if (!selectedAnnotation) return;
    const selection = selectedAnnotation;
    if (selection.note) {
      if (selection.kind === 'draft') discardNoteDraft();
      else submitThreadState({thread:selection.thread, target:selection.target}, 'withdrawn');
      return;
    }
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
    positionAnnotationBubbles(selection.group?.closest('.fragment-stage') || document);
    try {
      await persistAnnotationAnchor(selection.thread, anchor);
    } catch (error) {
      location.reload();
    }
  }

  async function useTool(mode, fragment = activeFragment) {
    cancelDrawing();
    clearAnnotationSelection();
    // An open comment covers the surface the reviewer is about to draw on, so
    // arming a tool hands the pointer back to the content.
    if (mode !== 'select') closeAnnotationBubbles();
    setSelectedTool(mode);
    if (mode === 'select') {
      if (annotationDraft?.shapeDraft) syncShapeDraft(true);
      return;
    }
    if (fragment) setActiveFragment(fragment);
    if (!fragment) {
      const label = q('[data-tool-target]');
      if (label) label.textContent = 'Choose an explanation first';
      resetTool();
      return;
    }
    // There is nothing to draw on until the explanation has arrived, so arming
    // a tool waits for its content rather than silently disarming itself.
    if (fragment.dataset.fragmentHref) await hydrateFragment(fragment);
    if (fragment.dataset.fragmentHref) {
      const label = q('[data-tool-target]');
      if (label) label.textContent = 'This explanation is still loading';
      resetTool();
      return;
    }
    if (mode === 'target') {
      openAnnotation({type:'target'}, {fragment, anchorElement:q('.fragment-head', fragment)});
      return;
    }
    if (mode === 'text') {
      const selection = getSelection();
      const selectable = q('[data-selectable]', fragment);
      if (!selection || selection.isCollapsed || !selectable || !selectable.contains(selection.anchorNode) || !selectable.contains(selection.focusNode)) {
        const label = q('[data-tool-target]');
        if (label) label.textContent = 'Select some text first';
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
      openAnnotation({type:'text',text:{exact,start,end:start+exact.length,prefix:allText.slice(Math.max(0,start-32),start),suffix:allText.slice(start+exact.length,start+exact.length+32),color:annotationColor}}, {fragment, anchorRect:selectedRange.getBoundingClientRect()});
      return;
    }
    const overlay = q('.review-overlay', fragment);
    if (!overlay) {
      resetTool();
      return;
    }
    // A nearby composer should not intercept the next stroke in a multi-shape
    // annotation. Keep its draft values, hide it while the tool is armed, and
    // let addDraftShape reopen it beside the newly completed mark.
    if (annotationDraft?.shapeDraft) {
      const form = q('.annotation-compose');
      annotationDraft.body = q('[name=body]', form)?.value || annotationDraft.body || '';
      form?.classList.remove('open');
      resetAnnotationComposerPosition();
    }
    if (mode === 'sticky') {
      // A sticky is paper, not ink, so it opens on the note palette until the
      // reviewer picks a colour of their own.
      if (!annotationColorTouched) {
        annotationColor = noteDefaultColor;
        const picker = q('[data-annotation-color]');
        if (picker) picker.value = noteDefaultColor;
      }
      overlay.classList.add('placing');
    }
    overlay.classList.add('drawing');
    drawing = {fragment, overlay, mode, color:annotationColor, points: []};
  }

  document.addEventListener('mousedown', event => {
    if (event.target.closest('[data-tool="text"]')) event.preventDefault();
  });

  document.addEventListener('pointerover', event => {
    const fragment = event.target.closest('.fragment');
    if (fragment && !drawing) setActiveFragment(fragment);
    if (event.pointerType !== 'touch' && fragment && !(event.relatedTarget instanceof Node && fragment.contains(event.relatedTarget))) {
      beginFragmentPrefetch(fragment, 'pointer');
    }
    if (!drawing) revealAnnotationBubble(annotationBubbleAt(event.target));
  });

  document.addEventListener('pointerout', event => {
    const fragment = event.target.closest('.fragment');
    if (event.pointerType !== 'touch' && fragment && !(event.relatedTarget instanceof Node && fragment.contains(event.relatedTarget))) {
      endFragmentPrefetch(fragment, 'pointer');
    }
    hideAnnotationBubbleSoon(annotationBubbleAt(event.target));
  });

  document.addEventListener('focusin', event => {
    const fragment = event.target.closest('.fragment');
    if (fragment) setActiveFragment(fragment);
    if (fragment && !(event.relatedTarget instanceof Node && fragment.contains(event.relatedTarget))) beginFragmentPrefetch(fragment, 'focus');
    revealAnnotationBubble(annotationBubbleAt(event.target));
  });

  document.addEventListener('focusout', event => {
    const fragment = event.target.closest('.fragment');
    if (fragment && !(event.relatedTarget instanceof Node && fragment.contains(event.relatedTarget))) endFragmentPrefetch(fragment, 'focus');
    hideAnnotationBubbleSoon(annotationBubbleAt(event.target));
  });

  document.addEventListener('click', event => {
    const retryFile = event.target.closest?.('[data-retry-file]');
    if (retryFile) {
      void hydrateReviewFile(retryFile.closest('[data-file-diff-href]'), {force:true});
      return;
    }
	const nextAuxiliaryPage = event.target.closest?.('[data-aux-file-next]');
	if (nextAuxiliaryPage) {
	  void appendAuxiliaryDiff(nextAuxiliaryPage);
	  return;
	}
    const retrySurface = event.target.closest?.('[data-retry-surface]');
    if (retrySurface) {
      void hydrateReviewSurface(retrySurface.dataset.retrySurface, {force:true});
      return;
    }
    const nextSurfacePage = event.target.closest?.('[data-surface-next]');
    if (nextSurfacePage) {
      event.preventDefault();
      const surface = nextSurfacePage.closest('[data-review-surface]');
      if (surface?.dataset.reviewSurface === 'manifest') beginContinuousCoverageLoad(surface);
      else void loadReviewSurfacePage(nextSurfacePage);
      return;
    }
    const boundedLink = event.target.closest?.('a[href]');
    if (boundedLink && !boundedLink.hasAttribute('data-open-fragment') && !boundedLink.getAttribute('href')?.startsWith('#') && !event.defaultPrevented && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey && (!boundedLink.target || boundedLink.target === '_self')) {
      const destination = new URL(boundedLink.href, location.href);
      const view = destination.searchParams.get('view');
      if (destination.origin === location.origin && destination.pathname === location.pathname && (view === 'code' || view === 'manifest')) {
        event.preventDefault();
		if (boundedLink.closest('.diff-drawer.open')) closeDrawer();
        history.pushState({view}, '', destination);
        setView(view, false);
        return;
      }
    }
    const fragmentDrawerLink = event.target.closest?.('[data-open-fragment]');
    if (fragmentDrawerLink) {
      event.preventDefault();
      openFragmentDrawer(fragmentDrawerLink.dataset.openFragment, fragmentDrawerLink);
      return;
    }
    const sagaLink = event.target.closest?.('a[href^="#"]');
    // A hydrated Markdown citation is still an in-page link, but linked-code
    // activation takes precedence over its original footnote navigation.
    if (sagaLink && !sagaLink.hasAttribute('data-open-diffs')) {
	  event.preventDefault();
      const id = decodeURIComponent(sagaLink.getAttribute('href').slice(1));
	  const sagaURL = new URL(location.href);
	  ['view', 'file', 'diff', 'mode'].forEach(key => sagaURL.searchParams.delete(key));
	  sagaURL.hash = id;
	  history.pushState({view:'saga'}, '', sagaURL);
      // pushState deliberately does not dispatch hashchange or perform native
      // anchor scrolling. Run the same lazy reveal, view switch, highlight,
      // and scroll path used for initial and browser-history navigation.
	  if (id) void activateLandmark().then(revealHashedAnnotationBubble);
	  else setView('saga', false);
      return;
    }
    const bubbleToggle = event.target.closest?.('[data-annotation-bubble-toggle]');
    if (bubbleToggle) { pinAnnotationBubble(bubbleToggle.closest('[data-annotation-bubble]')); return; }
    if (pinnedBubble && !event.target.closest?.('[data-annotation-bubble]')) {
      const unpinned = pinnedBubble;
      pinnedBubble = null;
      hideAnnotationBubbleSoon(unpinned);
    }
    const permalink = event.target.closest('[data-copy-link]');
    if (permalink) { copyPermalink(permalink); return; }
    if (event.target.closest('[data-undo]')) { performHistoryAction('undo'); return; }
    if (event.target.closest('[data-redo]')) { performHistoryAction('redo'); return; }
    if (event.target.closest('[data-remove-annotation]')) { removeSelectedAnnotation(); return; }
    if (event.target.closest('[data-commit-note]')) { commitNoteDraft(); return; }
    const saveNote = event.target.closest('[data-save-note]');
    if (saveNote) { endNoteEdit(saveNote.closest('.sticky-note'), true); return; }
    const cancelNote = event.target.closest('[data-cancel-note]');
    if (cancelNote) {
      const note = cancelNote.closest('.sticky-note');
      if (note.classList.contains('pending')) discardNoteDraft(); else endNoteEdit(note, false);
      return;
    }
    const stickyNote = event.target.closest('[data-sticky-note]');
    if (stickyNote) { selectStickyNote(stickyNote); return; }
    const annotation = event.target.closest('.annotation.selectable');
    if (annotation) { selectAnnotation(annotation); return; }
    const docTwisty = event.target.closest('[data-doc-twisty]');
    if (docTwisty) { toggleDocNode(docTwisty); return; }
    const chapterToggle = event.target.closest('[data-chapter-toggle]');
    if (chapterToggle) { toggleChapter(chapterToggle); return; }
    const viewTab = event.target.closest('[data-view-tab]');
    if (viewTab) { setView(viewTab.dataset.viewTab); return; }
    const manifestMode = event.target.closest('[data-manifest-mode]');
    if (manifestMode) { void activateManifestMode(manifestMode.dataset.manifestMode); return; }
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
    // Scoped to the toolbar buttons, otherwise every click inside a diff surface
    // matches its data-layout container and never reaches the line handlers below.
    const layout = event.target.closest('button[data-layout]');
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
    const targetCodeButton = event.target.closest('[data-target-code-href]');
    if (targetCodeButton) { event.preventDefault(); void hydrateTargetCode(targetCodeButton); return; }
    const drawerButton = event.target.closest('[data-open-diffs]');
    if (drawerButton) { event.preventDefault(); openDrawer(drawerButton.dataset.openDiffs, drawerButton); return; }
    if (event.target.closest('[data-close-drawer]')) { closeDrawer(); return; }
    const annotationTools = event.target.closest('[data-annotation-tools]');
    if (annotationTools) { void toggleAnnotationTools(annotationTools); return; }
    const reviewDecision = event.target.closest('[data-review-decision]');
    if (reviewDecision) { activateReviewDecision(reviewDecision); return; }
    const reviewComment = event.target.closest('[data-review-comment]');
    if (reviewComment) { openReviewComment(reviewComment.closest('[data-review-controls]') || {dataset:{reviewTarget:reviewComment.dataset.reviewComment,reviewTitle:reviewComment.dataset.reviewTitle}}); return; }
    const reviewCancel = event.target.closest('[data-review-cancel]');
    if (reviewCancel) { closeReviewComposer(reviewCancel.closest('[data-review-decision-form]')); return; }
    if (event.target.closest('[data-close-annotation]')) { closeAnnotation(); return; }
    const diffAction = event.target.closest('[data-diff-action]');
    if (diffAction) { openDiffComposer(diffActionContext(diffAction)); return; }
    if (event.target.closest('[data-close-diff-compose]')) { q('.diff-compose').classList.remove('open'); return; }
    const tool = event.target.closest('[data-tool]');
    if (tool) { void useTool(tool.dataset.tool, tool.closest('.fragment')); return; }
    const fragment = event.target.closest('.fragment');
    if (fragment) setActiveFragment(fragment);
  });

  document.addEventListener('dblclick', event => {
    const note = event.target.closest?.('[data-sticky-note]');
    if (!note || note.classList.contains('pending')) return;
    selectStickyNote(note);
    beginNoteEdit(note);
  });

  document.addEventListener('toggle', event => {
    const details = event.target.closest?.('details');
    if (!details) return;
    if (!details.open) {
      if (details.dataset.fileDiffHref) cancelReviewFile(details);
      return;
    }
    if (details.dataset.fileDiffHref) void hydrateReviewFile(details);
    if (details.dataset.coverageFileHref) void hydrateCoverageFile(details);
    if (details.dataset.coverageTargetHref) void hydrateCoverageTarget(details);
    hydrateOpenedManifestDiffs(details);
  }, true);

  document.addEventListener('submit', event => {
    const form = event.target;
    if (form.matches('[data-review-decision-form]')) {
      event.preventDefault();
      submitReviewComposer(form);
      return;
    }
    if (form.matches('form[action^="/api/"]')) {
      let token = q('[name=mutation_token]', form);
      if (!token) {
        token = document.createElement('input');
        token.type = 'hidden';
        token.name = 'mutation_token';
        form.append(token);
      }
      token.value = mutationToken;
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

  document.addEventListener('input', event => {
    if (event.target.matches?.('[data-file-filter]')) filterTree();
    if (event.target.matches?.('[data-manifest-filter]')) filterManifest();
  });

  document.addEventListener('change', event => {
    if (event.target.matches?.('[data-hide-reviewed]')) filterTree();
  });

  document.addEventListener('keydown', event => {
    const editingField = event.target.closest?.('.sticky-note-text');
    if (editingField) {
      const note = editingField.closest('.sticky-note');
      const pending = note.classList.contains('pending');
      if (event.key === 'Escape') {
        event.preventDefault();
        if (pending) discardNoteDraft(); else endNoteEdit(note, false);
      } else if (event.key === 'Enter' && (event.metaKey || event.ctrlKey)) {
        event.preventDefault();
        if (pending) commitNoteDraft(); else endNoteEdit(note, true);
      }
      return;
    }
    if (selectedAnnotation && annotationDeleteShortcut(event)) {
      event.preventDefault();
      removeSelectedAnnotation();
      return;
    }
    if (selectedAnnotation?.note) {
      if (event.key === 'Enter' || event.key === 'F2') {
        event.preventDefault();
        const element = selectedAnnotation.element;
        if (element.classList.contains('pending')) q('.sticky-note-text', element)?.focus();
        else beginNoteEdit(element);
        return;
      }
      const step = event.shiftKey ? .05 : .01;
      const nudge = {ArrowLeft:[-step,0], ArrowRight:[step,0], ArrowUp:[0,-step], ArrowDown:[0,step]}[event.key];
      if (nudge) {
        event.preventDefault();
        nudgeSelectedNote(nudge[0], nudge[1]);
        return;
      }
    }
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
    const workspaceTab = event.target.closest?.('[data-view-tab]');
    if (workspaceTab && ['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) {
      const tabs = qa('[data-view-tab]');
      const index = tabs.indexOf(workspaceTab);
      const step = event.key === 'ArrowLeft' ? -1 : 1;
      const next = event.key === 'Home' ? tabs[0]
        : event.key === 'End' ? tabs[tabs.length - 1]
        : tabs[(index + step + tabs.length) % tabs.length];
      event.preventDefault();
      setView(next.dataset.viewTab);
      next.focus();
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
    const focusedBubble = document.activeElement?.closest?.('[data-annotation-bubble]') || pinnedBubble;
    if (focusedBubble) {
      closeAnnotationBubble(focusedBubble);
      return;
    }
    if (selectedAnnotation) {
      clearAnnotationSelection();
      return;
    }
    if (annotationDraft?.noteDraft) {
      discardNoteDraft();
      return;
    }
    closeDrawer();
    closeAnnotation();
    updateLineSelection([]);
    const diffForm = q('.diff-compose');
    if (diffForm) diffForm.classList.remove('open');
    qa('[data-review-decision-form].open').forEach(form => closeReviewComposer(form));
  });

  document.addEventListener('keyup', event => {
    if (noteNudge && String(event.key).startsWith('Arrow')) commitNoteNudge();
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
    const note = event.target.closest?.('[data-sticky-note]');
    if (note && selectedTool === 'select' && !event.target.closest('.sticky-note-text,button')) {
      selectStickyNote(note);
      const box = note.closest('.fragment-stage').getBoundingClientRect();
      annotationDrag = {
        selection:selectedAnnotation,
        before:selectedAnchor(),
        start:{x:(event.clientX-box.left)/box.width,y:(event.clientY-box.top)/box.height},
        box,
        mode:'note',
        moved:false
      };
      note.setPointerCapture?.(event.pointerId);
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
    if (drawing.mode === 'sticky') {
      const fragment = drawing.fragment;
      const point = {x:clampNormalized((event.clientX-box.left)/box.width), y:clampNormalized((event.clientY-box.top)/box.height)};
      cancelDrawing();
      setSelectedTool('select');
      beginNoteDraft(fragment, point);
      event.preventDefault();
      return;
    }
    drawing.points = [{x:(event.clientX-box.left)/box.width,y:(event.clientY-box.top)/box.height}];
    drawing.start = drawing.points[0];
    drawing.preview = document.createElementNS('http://www.w3.org/2000/svg', drawing.mode === 'rect' ? 'rect' : 'polyline');
    drawing.preview.setAttribute('class', 'annotation pending ' + (drawing.mode === 'draw' ? 'path' : ''));
    drawing.preview.setAttribute('stroke', drawing.color);
    drawing.preview.setAttribute('fill', drawing.mode === 'rect' ? drawing.color : 'none');
    if (drawing.mode === 'rect') drawing.preview.setAttribute('fill-opacity', '.22');
    drawing.overlay.append(drawing.preview);
    event.preventDefault();
  });

  document.addEventListener('pointermove', event => {
    if (annotationDrag) {
      const point = {x:(event.clientX-annotationDrag.box.left)/annotationDrag.box.width,y:(event.clientY-annotationDrag.box.top)/annotationDrag.box.height};
      const dx = point.x-annotationDrag.start.x, dy = point.y-annotationDrag.start.y;
      const anchor = cloneAnchor(annotationDrag.before);
      if (annotationDrag.mode === 'note') {
        anchor.note = translateNote(anchor.note, dx, dy);
        renderNoteElement(annotationDrag.selection.element, anchor.note);
      } else {
        if (annotationDrag.mode === 'resize') {
          const shape = anchor.shapes[annotationDrag.selection.index];
          shape.width = Math.max(.01, Math.min(1-shape.x, point.x-shape.x));
          shape.height = Math.max(.01, Math.min(1-shape.y, point.y-shape.y));
        } else {
          anchor.shapes[annotationDrag.selection.index] = translateShape(anchor.shapes[annotationDrag.selection.index], dx, dy);
        }
        renderShapeElement(annotationDrag.selection.element, anchor.shapes[annotationDrag.selection.index]);
        updateAnnotationResizeHandle();
      }
      if (annotationDrag.selection.kind === 'draft') annotationDraft.anchor = anchor;
      annotationDrag.after = anchor;
      annotationDrag.moved = Math.abs(dx) + Math.abs(dy) > .001;
      positionAnnotationBubbles(annotationDrag.selection.element.closest('.fragment-stage') || document);
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
        if (!annotationDraft.noteDraft) q('[name=anchor]', q('.annotation-compose')).value = JSON.stringify(annotationDraft.anchor);
        updateHistoryControls();
      } else {
        try {
          await persistAnnotationAnchor(drag.selection.thread, drag.after);
        } catch (error) {
          if (drag.selection.note) renderNoteElement(drag.selection.element, drag.before.note);
          else renderShapeElement(drag.selection.element, drag.before.shapes[drag.selection.index]);
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
  updateReviewProgress();
  prepareLandmarks();
  prepareDiffCitations();
  prepareTextHighlights();
  const shellArriving = observeDeferredFragments();

  const firstFragment = q('.fragment');
  if (firstFragment) setActiveFragment(firstFragment);
  const reviewProgressMap = q('[data-review-progress]');
  reviewProgressMap?.addEventListener('pointerover', event => {
    const segment = event.target.closest?.('[data-review-progress-target]');
    if (segment) showReviewProgressTooltip(segment);
  });
  reviewProgressMap?.addEventListener('pointerleave', () => hideReviewProgressTooltip(reviewProgressMap));
  reviewProgressMap?.addEventListener('focusin', event => {
    const segment = event.target.closest?.('[data-review-progress-target]');
    if (segment) showReviewProgressTooltip(segment);
  });
  reviewProgressMap?.addEventListener('focusout', event => {
    if (!reviewProgressMap.contains(event.relatedTarget)) hideReviewProgressTooltip(reviewProgressMap);
  });
  q('[data-file-filter]')?.addEventListener('input', filterTree);
  q('[data-hide-reviewed]')?.addEventListener('change', filterTree);
  q('[data-manifest-filter]')?.addEventListener('input', filterManifest);
  q('[data-annotation-color]')?.addEventListener('input', event => { annotationColorTouched = true; annotationColor = normalizedAnnotationColor(event.target.value); });
  q('[data-annotation-color]')?.addEventListener('change', event => { if (selectedAnnotation) recolorSelectedAnnotation(event.target.value); });
  prepareContext();
  highlightCode();
  applyDiffLayout('inline');
  addEventListener('resize', () => { applyDiffLayout(diffLayout); positionFragmentOverlays(); });
  addEventListener('scroll', () => {
    const progress = q('[data-review-progress]');
    if (!progress) return;
    progress.classList.add('scrolling');
    clearTimeout(reviewScrollTimer);
    reviewScrollTimer = setTimeout(() => progress.classList.remove('scrolling'), 650);
  }, {passive:true});
  const requestedView = new URL(location.href).searchParams.get('view');
  const initialView = requestedView === 'code' || requestedView === 'manifest' ? requestedView : 'saga';
  setView(initialView, false);
  setManifestMode('code');
  const anchorResolving = initialView === 'saga'
    ? activateLandmark().then(revealHashedAnnotationBubble)
    : hydrateReviewSurface(initialView).then(revealHashedAnnotationBubble);
  // The page arrives as a shell and fills in what is on screen. Saying when
  // that has finished is the difference between a reviewer who can see the
  // page has settled and automation that would otherwise have to guess.
  void Promise.all([shellArriving, anchorResolving]).then(() => {
    document.body.dataset.shellReady = 'true';
  });
  positionFragmentOverlays();
  globalThis.requestAnimationFrame?.(positionFragmentOverlays);
  addEventListener('hashchange', () => {
    const view = new URL(location.href).searchParams.get('view');
    if (view === 'code' || view === 'manifest') void hydrateReviewSurface(view).then(revealHashedAnnotationBubble);
    else void activateLandmark().then(revealHashedAnnotationBubble);
  });
  addEventListener('popstate', () => {
    const view = new URL(location.href).searchParams.get('view');
    setView(view === 'code' || view === 'manifest' ? view : 'saga', false);
  });
})();`
