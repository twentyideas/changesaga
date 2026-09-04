package server

// darkTokens recolours the design tokens for dark mode. Only the palette
// changes: every rule in pageStyles already reads through these variables, so
// spacing, type and layout stay shared with light mode and cannot drift apart.
// The values track GitHub's dark palette because the light tokens above track
// its light one, which keeps text, diff and status colours at AA contrast.
const darkTokens = `
--bg:#0d1117;--bg-subtle:#161b22;--bg-inset:#1c2128;--ink:#e6edf3;--muted:#9198a1;--faint:#8b949e;
--line:#30363d;--line-soft:#21262d;--accent:#4493f8;--accent-soft:#121d2f;--accent-line:#316dca;
--green:#3fb950;--red:#f85149;--amber:#d29922;--sel:#132132;
--add-bg:#12261e;--add-line:#3fb950;--del-bg:#25171c;--del-line:#f85149;--code-bg:#0d1117;--code-gutter:#161b22;
--warning-bg:#2d240c;--warning-line:#9e6a03;--danger-bg:#25171c;--danger-line:#f85149;
--frosted-bg:#0d1117ed;--toolbar-bg:#0d1117ee;--accent-hover-bg:#1c2d41;--accent-hover-ink:#79c0ff;
--landmark-bg:#1c2733;--footnote-hover-bg:#1c2d41;--citation-bg:#121d2f;--landmark-affordance-bg:#161b22f2;
--button-hover:#30363d;--primary-bg:#1f6feb;--primary-hover:#1158c7;--primary-ink:#fff;
--approve-ink:#56d364;--approve-bg:#12261e;--approve-hover:#173f2b;--reject-ink:#ff7b72;--reject-bg:#25171c;--reject-hover:#321c22;
--suggestion-ink:#aff5b4;--text-anchor-bg:#3d2f00;--active-fragment-line:#316dca;
--add-gutter:#142c22;--del-gutter:#321c22;--copied-bg:#30363d;--copied-ink:#e6edf3;
--syntax-keyword:#ff7b72;--syntax-string:#a5d6ff;--syntax-number:#79c0ff;--syntax-comment:#8b949e;
--syntax-type:#ffa657;--syntax-property:#d2a8ff;--syntax-punctuation:#c9d1d9;
--shadow:0 6px 24px #01040966,0 1px 3px #010409aa;color-scheme:dark
`

// pageStyles is the whole renderer stylesheet. The design target is a quiet
// developer tool: system UI type for chrome, monospace for code and code-shaped
// metadata, hairline separators instead of cards, and controls that stay
// invisible until the reviewer hovers or focuses the thing they belong to.
const pageStyles = `
:root{
--bg:#ffffff;--bg-subtle:#f6f8fa;--bg-inset:#eef1f4;--ink:#1f2328;--muted:#59636e;--faint:#59636e;
--line:#d1d9e0;--line-soft:#e7ebef;--accent:#0969da;--accent-soft:#ddf4ff;--accent-line:#54aeff;
--green:#116329;--red:#a40e26;--amber:#9a6700;--sel:#eaf3fe;
--add-bg:#e6ffec;--add-line:#2da44e;--del-bg:#ffebe9;--del-line:#cf222e;--code-bg:#ffffff;--code-gutter:#f6f8fa;
--warning-bg:#fff8e6;--warning-line:#e0c98a;--danger-bg:#fff5f4;--danger-line:#e5b3ae;
--frosted-bg:#ffffffed;--toolbar-bg:#ffffffee;--accent-hover-bg:#cfeaff;--accent-hover-ink:#0550ae;
--landmark-bg:#f5f9ff;--footnote-hover-bg:#dbeafe;--citation-bg:#e8f2ff;--landmark-affordance-bg:#fffffff2;
--button-hover:#e2e6ea;--primary-bg:#0969da;--primary-hover:#0860c4;--primary-ink:#fff;
--approve-ink:#2da44e;--approve-bg:#e6ffec80;--approve-hover:#dafbe1;--reject-ink:#cf222e;--reject-bg:#ffebe980;--reject-hover:#ffcecb;
--suggestion-ink:#0a3622;--text-anchor-bg:#fff3c4;--active-fragment-line:#c8e1ff;
--add-gutter:#d9f6e0;--del-gutter:#ffdcd8;--copied-bg:#1f2328;--copied-ink:#fff;
--syntax-keyword:#cf222e;--syntax-string:#0a3069;--syntax-number:#0550ae;--syntax-comment:#6e7781;
--syntax-type:#953800;--syntax-property:#8250df;--syntax-punctuation:#57606a;
--ui:-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,"Helvetica Neue",Arial,sans-serif;
--mono:ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,"Liberation Mono",monospace;
--top:44px;--shadow:0 6px 24px #1f232814,0 1px 3px #1f23281f;--radius:6px;color-scheme:light
}
/* Dark mode. The OS preference decides unless the reviewer has pinned a theme,
   which is why the media rule excuses an explicit light choice. ------------ */
@media (prefers-color-scheme:dark){:root:not([data-theme=light]){` + darkTokens + `}}
:root[data-theme=dark]{` + darkTokens + `}
*{box-sizing:border-box}
html{scroll-behavior:smooth}
body{margin:0;background:var(--bg);color:var(--ink);font:13px/1.55 var(--ui);-webkit-font-smoothing:antialiased}
button,input,textarea,select{font:inherit;color:inherit}
button{cursor:pointer}
a{color:var(--accent)}
:focus-visible{outline:2px solid var(--accent);outline-offset:1px;border-radius:3px}
.sr-only{position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0}
.icon-sprite{display:block}
.i{width:16px;height:16px;flex:none;display:block}
.ficon{width:14px;height:14px;flex:none}

/* Slide-native review ---------------------------------------------------- */
.slide-shell{grid-template-columns:244px minmax(0,1fr);height:calc(100vh - var(--top));min-height:0;overflow:hidden;background:#111}
.slide-shell.slide-sidebar-collapsed{grid-template-columns:0 minmax(0,1fr)}
.slide-shell.slide-sidebar-collapsed .slide-sidebar{visibility:hidden;overflow:hidden;padding-inline:0;border:0}
.slide-shell.code-mode{grid-template-columns:280px minmax(0,1fr);height:auto;min-height:calc(100vh - var(--top));overflow:visible;background:var(--bg)}
.slide-shell.code-mode .slide-sidebar{visibility:visible;overflow:auto;padding:10px 8px 40px;border-right:1px solid var(--line)}
.slide-shell:not(.code-mode)>.content{width:100%;height:100%;padding:0;overflow:hidden}
.slide-shell:not(.code-mode) #view-saga{height:100%}
.slide-sidebar{position:relative;top:auto;height:100%;padding:8px 10px 32px;background:var(--bg-subtle);transition:visibility .14s}
.slide-sidebar-toggle{margin-left:-6px}
.slide-present{margin-left:auto;border:1px solid var(--line);border-radius:6px;height:30px;align-self:center;padding-inline:12px}
.slide-side>.sidebar-title{align-items:flex-start;margin-bottom:8px;font-size:12px;line-height:1.35}
.slide-rail{min-width:0;counter-reset:slide-thumbnail}
.slide-rail-deck{min-width:0;margin:0 0 16px}
.slide-rail-deck>h2{position:sticky;top:-8px;z-index:3;margin:0 -2px 7px;padding:7px 4px 5px;background:var(--bg-subtle);color:var(--muted);font:600 11px/1.2 var(--ui);letter-spacing:.025em;text-transform:uppercase}
.slide-thumbnail-list{display:grid;grid-template-columns:minmax(0,1fr);min-width:0;gap:10px}
.slide-thumbnail-card{position:relative;min-width:0;counter-increment:slide-thumbnail;padding-left:22px;color:var(--muted)}
.slide-thumbnail-card::before{content:counter(slide-thumbnail);position:absolute;left:0;top:4px;width:17px;text-align:right;color:var(--faint);font:10px/1 var(--mono)}
.slide-thumbnail-preview{position:relative;width:100%;aspect-ratio:16/9;overflow:hidden;border:2px solid var(--line);border-radius:5px;background:#fff;box-shadow:0 1px 2px #1f23281f;transition:border-color .12s,box-shadow .12s}
.slide-thumbnail-preview iframe,.slide-thumbnail-preview img{display:block;width:100%;height:100%;border:0;object-fit:contain;pointer-events:none}
.slide-thumbnail-caption{display:flex;align-items:center;gap:5px;margin-top:4px}
.slide-thumbnail-title{display:block;min-width:0;flex:1;color:inherit;font:11.5px/1.3 var(--ui);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.slide-thumbnail-status{position:relative;z-index:3;display:grid;place-items:center;width:16px;height:16px;flex:none;border:1px solid var(--line);border-radius:50%;background:var(--bg);color:var(--faint);font:700 10px/1 var(--ui);pointer-events:none}
.slide-thumbnail-status::before{content:'·'}
.slide-thumbnail-status[data-review-state=approved]{border-color:var(--green);background:var(--approve-bg);color:var(--green)}
.slide-thumbnail-status[data-review-state=approved]::before{content:'✓'}
.slide-thumbnail-status[data-review-state=rejected]{border-color:var(--red);background:var(--reject-bg);color:var(--red)}
.slide-thumbnail-status[data-review-state=rejected]::before{content:'!'}
.slide-thumbnail-hit{position:absolute;z-index:2;inset:0;width:100%;padding:0;border:0;border-radius:5px;background:transparent}
.slide-thumbnail-hit:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
.slide-thumbnail-card:hover .slide-thumbnail-preview{border-color:var(--faint)}
.slide-thumbnail-card.active{color:var(--ink);font-weight:600}
.slide-thumbnail-card.active .slide-thumbnail-preview{border-color:var(--accent);box-shadow:0 0 0 1px var(--accent)}
.slide-native{display:grid;place-items:center;width:100%;height:100%;overflow:hidden;background:#111}
.slide-native-stage{position:relative;width:min(100%,calc(177.7778vh - 78.2222px));aspect-ratio:16/9;overflow:hidden;background:var(--bg);box-shadow:var(--shadow)}
.slide-native-slide{position:absolute;z-index:1;inset:0;overflow:hidden;background:var(--bg)}
.slide-native-slide[hidden]{display:none}.slide-native-slide.active{display:block}
.slide-native-slide .fragment{margin:0;border:0;border-radius:0;height:100%;min-height:100%;background:transparent}
.slide-native-slide .fragment-head{position:absolute;z-index:7;right:12px;top:42px;border:0;background:var(--frosted-bg);border-radius:8px}
.slide-native-slide .fragment-head::before{content:'Review slide';align-self:center;padding-left:8px;color:var(--muted);font:600 10px/1 var(--ui);letter-spacing:.025em;text-transform:uppercase}
.slide-native-slide .fragment-head [data-review-controls]{opacity:1}
.slide-native-slide .fragment-stage{height:100%;min-height:100%;display:grid;place-items:center;padding:0}
.slide-native-slide .fragment-frame{width:100%;height:100%;min-height:0;border:0;border-radius:0}
.slide-native-slide .fragment-image{display:block;width:100%;height:100%;max-height:none;object-fit:contain}
.slide-native-header{position:absolute;z-index:6;left:12px;right:12px;top:10px;display:flex;align-items:center;justify-content:flex-end;gap:16px;pointer-events:none;color:var(--muted);text-shadow:0 1px 2px var(--bg)}
.slide-native-header>div{display:flex;align-items:baseline;gap:9px;min-width:0}
.slide-native-header strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--ink)}
.slide-native-header [data-slide-deck-title]{flex:none;font-size:11px;text-transform:uppercase;letter-spacing:.04em}
.slide-native-header [data-slide-position]{flex:none;font:11px var(--mono)}
.slide-native-controls{position:absolute;z-index:8;inset:0;pointer-events:none}
.slide-step{position:absolute;top:50%;display:grid;place-items:center;width:38px;height:56px;transform:translateY(-50%);border:1px solid var(--line);border-radius:8px;background:var(--frosted-bg);color:var(--ink);box-shadow:var(--shadow);font:34px/1 var(--ui);pointer-events:auto;opacity:.15;transition:opacity .14s,background .14s}
.slide-step:hover,.slide-step:focus-visible{opacity:1;background:var(--bg)}
.slide-step:disabled{visibility:hidden}.slide-step[data-slide-previous]{left:12px}.slide-step[data-slide-next]{right:12px}
.slide-exit-presentation{position:absolute;z-index:10;right:16px;bottom:16px;padding:7px 11px;border:1px solid #ffffff55;border-radius:6px;background:#111b;color:#fff;opacity:0;transition:opacity .15s}
body.presentation-mode{overflow:hidden;background:#000}
body.presentation-mode>.topbar,body.presentation-mode .slide-shell>.slide-sidebar,body.presentation-mode .manifest-view,body.presentation-mode .annotation-toolbox{display:none}
body.presentation-mode .slide-shell{display:block;width:100vw;height:100vh;min-height:0;background:#000}
body.presentation-mode .slide-shell:not(.code-mode)>.content{width:100vw;height:100vh}
body.presentation-mode .slide-native-stage{width:min(100vw,177.7778vh);height:auto;max-height:100vh;box-shadow:none}
body.presentation-mode .slide-native-header,body.presentation-mode .slide-native-slide .fragment-head{opacity:0;pointer-events:none}
body.presentation-mode .slide-native-slide .review-overlay,body.presentation-mode .slide-native-slide .landmark-hotspot,body.presentation-mode .slide-native-slide .sticky-note,body.presentation-mode .slide-native-slide .annotation-bubble{display:none}
body.presentation-mode .slide-native-stage:hover .slide-step,body.presentation-mode .slide-step:focus-visible{opacity:.6}
body.presentation-mode .slide-native-stage:hover .slide-exit-presentation,body.presentation-mode .slide-exit-presentation:focus-visible{opacity:1}
@media(max-width:780px){.slide-shell{display:grid;grid-template-columns:min(210px,42vw) minmax(0,1fr)}.slide-shell.slide-sidebar-collapsed{grid-template-columns:0 minmax(0,1fr)}.slide-sidebar{position:relative;max-height:none}.slide-native-header{left:8px;right:8px}.slide-native-header strong{display:none}.slide-step{width:30px;height:46px}.slide-step[data-slide-previous]{left:6px}.slide-step[data-slide-next]{right:6px}}

/* Top bar ---------------------------------------------------------------- */
.topbar{position:sticky;top:0;z-index:30;height:var(--top);display:flex;align-items:center;gap:14px;padding:0 12px;background:var(--bg);border-bottom:1px solid var(--line)}
.brand{display:flex;align-items:center;gap:6px;color:var(--muted);font:600 11px var(--mono);letter-spacing:.04em}
.brand .i{width:14px;height:14px}
.view-tabs{display:flex;align-self:stretch;gap:2px}
.view-tab{display:flex;align-items:center;gap:6px;border:0;border-bottom:2px solid transparent;border-radius:0;padding:0 10px;background:transparent;color:var(--muted);font-size:12.5px}
.view-tab:hover{color:var(--ink);background:var(--bg-subtle)}
.view-tab.active{color:var(--ink);border-color:var(--accent);font-weight:600}
.view-tab-count{display:inline-grid;place-items:center;min-width:18px;height:18px;padding:0 5px;border-radius:99px;background:var(--bg-inset);color:var(--faint);font:10px var(--mono)}
.view-tab.active .view-tab-count,.activity-trigger[aria-expanded=true] .view-tab-count{background:var(--accent-soft);color:var(--accent)}
.activity-trigger{align-self:stretch}
.activity-trigger[aria-expanded=true]{color:var(--ink);border-color:var(--accent);font-weight:600}
.top-meta{margin-left:auto;color:var(--faint);font:11px var(--mono)}
.top-meta[hidden]{display:none}
.theme-toggle{margin-left:8px}
.review-progress{position:relative;display:flex;align-items:stretch;gap:2px;flex:0 1 280px;height:12px;margin-left:auto;padding:3px;border:1px solid var(--line-soft);border-radius:99px;background:var(--bg-subtle);opacity:.42;transition:height .18s ease,opacity .28s ease,box-shadow .2s ease}
.top-meta:not([hidden])+.review-progress{margin-left:0}
.review-progress:hover,.review-progress:focus-within{height:16px;opacity:1;box-shadow:0 2px 8px #1f23281f}
.review-progress-segment{min-width:2px;flex:1 1 0;border-radius:2px;background:var(--line);transition:flex-grow .16s ease,background .2s ease,transform .16s ease;outline-offset:1px}
.review-progress-segment.approved{background:var(--green)}
.review-progress-segment.rejected{background:var(--red)}
.review-progress-segment:hover,.review-progress-segment:focus-visible{flex-grow:3;transform:scaleY(1.35)}
.review-progress-tooltip{position:absolute;z-index:2;top:calc(100% + 8px);right:0;width:max-content;max-width:min(380px,calc(100vw - 24px));padding:8px 10px;border:1px solid #ffffff20;border-radius:6px;background:#24292f;color:#fff;box-shadow:var(--shadow);font:12px/1.4 var(--ui);pointer-events:none}
.review-progress-tooltip[hidden]{display:none}
.review-progress-tooltip-head{display:flex;align-items:baseline;justify-content:space-between;gap:18px}
.review-progress-tooltip-head strong{font-weight:600}
.review-progress-tooltip-head span{color:#ffffffb8;white-space:nowrap;font-size:11px}
.review-progress-tooltip-note{display:block;margin-top:5px;padding-top:5px;border-top:1px solid #ffffff24;white-space:pre-wrap;overflow-wrap:anywhere;color:#ffffffeb}
.review-progress-tooltip-note[hidden]{display:none}
.review-progress.scrolling{opacity:.72}
.review-progress.changed{height:16px;opacity:1;box-shadow:0 0 0 2px var(--accent-soft),0 2px 10px #0969da44}

/* Shell ------------------------------------------------------------------ */
.shell{display:grid;grid-template-columns:264px minmax(0,1fr);min-height:calc(100vh - var(--top))}
.shell.code-mode{grid-template-columns:280px minmax(0,1fr)}
.shell.tree-hidden{grid-template-columns:0 minmax(0,1fr)}
.sidebar{position:sticky;top:var(--top);height:calc(100vh - var(--top));overflow:auto;padding:10px 8px 40px;background:var(--bg-subtle);border-right:1px solid var(--line)}
.tree-hidden .sidebar{overflow:hidden;padding-inline:0;border:0}
.sidebar-title{display:flex;align-items:center;gap:7px;padding:6px 8px;margin-bottom:4px;color:var(--ink);text-decoration:none;font-weight:600;font-size:13px;border-radius:var(--radius)}
.sidebar-title:hover{background:var(--bg-inset)}
.sidebar-title .i{color:var(--muted)}
.side-label{margin:14px 6px 4px;color:var(--faint);font:600 11px var(--ui);letter-spacing:.02em}

/* Documentation navigation tree ------------------------------------------ */
.doc-tree{display:block}
.doc-row{display:flex;align-items:center;min-height:26px;border-radius:var(--radius)}
.doc-row:hover{background:var(--bg-inset)}
.doc-row.current{background:var(--sel)}
.doc-row.current>.doc-link{color:var(--accent);font-weight:600}
.doc-twisty{display:grid;place-items:center;width:20px;height:24px;flex:none;padding:0;border:0;background:transparent;color:var(--faint);border-radius:3px}
.doc-twisty:hover{color:var(--ink)}
.doc-twisty .i{width:13px;height:13px;transition:transform .12s ease}
.doc-twisty[aria-expanded=true] .i{transform:rotate(90deg)}
.doc-twisty.placeholder{visibility:hidden}
.doc-link{min-width:0;flex:1;padding:4px 8px 4px 0;color:var(--ink);text-decoration:none;font-size:13px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.doc-state{display:grid;place-items:center;width:20px;flex:none;color:var(--faint)}
.doc-state .i{width:12px;height:12px}
.doc-state.approved{color:var(--green)}
.doc-state.progress{color:var(--amber)}
.doc-children{margin-left:10px;border-left:1px solid var(--line);padding-left:4px}
.doc-children[hidden]{display:none}
.doc-children .doc-link{font-size:12.5px;color:var(--muted)}
.doc-children .doc-row:hover .doc-link{color:var(--ink)}

/* Changed-file tree ------------------------------------------------------ */
.tree-tools{display:flex;align-items:center;gap:4px;padding:2px 4px 6px}
.tree-search{position:relative;flex:1;display:flex;align-items:center}
.tree-search .i{position:absolute;left:7px;width:13px;height:13px;color:var(--faint);pointer-events:none}
.tree-tools input[type=search]{min-width:0;width:100%;height:26px;padding:0 8px 0 25px;border:1px solid var(--line);border-radius:var(--radius);background:var(--bg);font-size:12px}
.tree-tools input[type=search]::-webkit-search-cancel-button{-webkit-appearance:none}
.tree-filter-check{display:flex;align-items:center;gap:6px;margin:0 6px 6px;color:var(--muted);font-size:12px}
.tree-summary{margin:0 6px 6px;color:var(--faint);font:11px var(--mono)}
.tree-empty{margin:8px 6px;color:var(--muted);font-size:12px}
.file-tree{display:block;max-width:100%;overflow-x:auto;overscroll-behavior-x:contain;scrollbar-width:thin}
.file-tree summary,.file-tree a{display:flex;align-items:center;gap:6px;width:max-content;min-width:100%;min-height:24px;padding:0 6px 0 calc(4px + var(--depth,0) * 12px);border-radius:var(--radius);color:var(--ink);text-decoration:none;font:12px var(--mono);white-space:nowrap}
.file-tree summary{list-style:none;cursor:pointer;color:var(--muted)}
.file-tree summary::-webkit-details-marker{display:none}
.file-tree summary:hover,.file-tree a:hover{background:var(--bg-inset)}
.file-tree .twisty{width:12px;height:12px;flex:none;color:var(--faint);transition:transform .12s ease}
.file-tree details[open]>summary .twisty{transform:rotate(90deg)}
.file-tree .tree-name{min-width:max-content;overflow:visible;text-overflow:clip}
.file-tree .selected{background:var(--sel);box-shadow:inset 2px 0 var(--accent);color:var(--accent);font-weight:600}
.file-tree .tree-state{display:grid;place-items:center;width:14px;flex:none;color:transparent}
.file-tree .tree-state .i{width:12px;height:12px}
.file-tree .reviewed .tree-state{color:var(--green)}
.file-tree .reviewed .tree-name{color:var(--muted)}
.file-tree .counts{margin-left:auto;padding-left:8px;color:var(--faint);font:11px var(--mono)}

/* Content ---------------------------------------------------------------- */
.content{width:min(1080px,100%);padding:26px clamp(16px,3vw,40px) 96px}
.code-mode .content{width:100%;padding:0 0 40px}
.view{display:none}
.view.active{display:block}
.page-heading{margin:0 0 18px}
.page-heading h2{margin:0;font:600 22px/1.25 var(--ui);letter-spacing:-.01em}
.coverage-totals{margin:6px 0 0;color:var(--faint);font:11px var(--mono)}
.coverage-totals .gap{color:var(--red);font-weight:600}
.fragment-placeholder,.section-placeholder{margin:0;padding:8px 10px;color:var(--faint);font:11px var(--mono)}
.breadcrumbs{display:flex;align-items:center;gap:6px;margin:0 0 14px;color:var(--muted);font-size:12px}
.breadcrumbs a{color:var(--muted);text-decoration:none}
.breadcrumbs a:hover{color:var(--accent);text-decoration:underline}
.alert{display:flex;gap:9px;align-items:flex-start;margin:0 0 20px;padding:10px 12px;border:1px solid var(--warning-line);border-left:3px solid var(--amber);border-radius:var(--radius);background:var(--warning-bg);font-size:12.5px}
.alert .i{color:var(--amber);margin-top:1px}
.alert strong{display:block;margin-bottom:2px}
.remaining{margin:0;padding:24px;color:var(--muted);text-align:center;font-size:12.5px}

/* Chapter list ----------------------------------------------------------- */
.chapter-index{margin-top:28px;border-top:1px solid var(--line-soft);padding-top:14px}
.chapter-index>h2{margin:0 0 4px;font:600 12px var(--ui);color:var(--muted)}
.chapter-pages{border-bottom:1px solid var(--line-soft)}
.chapter-pages>.chapter{margin:0;padding:0;border-top:1px solid var(--line-soft)}
.chapter>.chapter-head{min-height:44px;padding:4px 2px}
.chapter-head h2{min-width:0;flex:1;font-size:15px}
.chapter-head h2 a{color:inherit;text-decoration:none}
.chapter-head h2 a:hover{color:var(--accent)}
.chapter-toggle{display:grid;place-items:center;width:26px;height:26px;padding:0;border:0;border-radius:4px;background:transparent;color:var(--faint)}
.chapter-toggle:hover{background:var(--bg-subtle);color:var(--ink)}
.chapter-toggle .twisty{transition:transform .14s ease}
.chapter.open>.chapter-head .chapter-toggle .twisty{transform:rotate(90deg)}
.chapter-body{padding:2px 0 22px 28px}
.chapter-body[hidden]{display:none}
.chapter-review-directory{position:sticky;top:calc(var(--top) + 8px);z-index:18;overflow:hidden;margin:4px 0 16px;padding:0;border:1px solid var(--accent-line);border-left:3px solid var(--accent);border-radius:9px;background:var(--frosted-bg);box-shadow:0 6px 20px #0969da1f;backdrop-filter:blur(8px)}
.chapter-review-directory>summary{display:flex;align-items:center;justify-content:space-between;gap:12px;min-height:38px;padding:7px 10px;cursor:pointer;list-style:none;background:var(--accent-soft);color:var(--accent);font:600 12px var(--ui);transition:background .14s ease,color .14s ease}
.chapter-review-directory>summary:hover{background:var(--accent-hover-bg);color:var(--accent-hover-ink)}
.chapter-review-directory>summary::-webkit-details-marker{display:none}
.review-directory-heading{display:flex;align-items:center;gap:7px;color:inherit}
.review-directory-heading .i{width:14px;height:14px;color:inherit}
.review-directory-heading .twisty{width:12px;height:12px;transition:transform .14s ease}
.chapter-review-directory[open]>summary .twisty{transform:rotate(90deg)}
.review-directory-meta{display:flex;align-items:center;gap:9px}
.review-directory-summary{color:var(--muted);font:11px var(--mono)}
.review-directory-hint{color:inherit;font:600 10px var(--ui);letter-spacing:.04em;text-transform:uppercase}
.review-directory-hint::after{content:"Show"}
.chapter-review-directory[open]>summary .review-directory-hint::after{content:"Hide"}
.review-directory-list{max-height:min(42vh,360px);overflow:auto;margin:0;padding:3px 6px 6px;border-top:1px solid var(--accent-line);background:var(--bg);list-style:none}
.review-directory-item{display:grid;grid-template-columns:10px minmax(130px,1fr) auto auto auto;align-items:center;gap:7px;min-height:38px;padding:3px 4px 3px calc(6px + var(--review-depth,0) * 12px);border-radius:6px}
.review-directory-item:hover,.review-directory-item:focus-within{background:var(--bg-subtle)}
.review-directory-state{width:8px;height:8px;border:1px solid var(--faint);border-radius:50%;background:var(--bg)}
.review-directory-item.approved .review-directory-state{border-color:var(--green);background:var(--green)}
.review-directory-item.changes-requested .review-directory-state{border-color:var(--red);background:var(--red)}
.review-directory-item>a{display:grid;min-width:0;color:inherit;text-decoration:none}
.review-directory-kind{color:var(--faint);font:9px var(--mono);letter-spacing:.04em;text-transform:uppercase}
.review-directory-title{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font:12.5px var(--ui)}
.review-directory-item.approved .review-directory-title{color:var(--muted)}
.review-directory-status{white-space:nowrap;color:var(--faint);font:10.5px var(--ui)}
.review-directory-item.approved .review-directory-status{color:var(--green)}
.review-directory-item.changes-requested .review-directory-status{color:var(--red)}
.review-directory-comments{display:flex;align-items:center;gap:2px;min-width:24px;color:var(--faint);font:10px var(--mono);text-decoration:none}
.review-directory-comments .i{width:12px;height:12px}
a.review-directory-comments:hover{color:var(--accent)}
.review-directory-item .review-controls{opacity:1}
.review-directory-item .review-comment{display:none}
.review-directory-item .review-decision-note{display:none}
.review-directory-empty{margin:0;padding:8px 10px;border-top:1px solid var(--accent-line);background:var(--bg);color:var(--faint);font:11px var(--ui)}

/* Sections and fragments ------------------------------------------------- */
.section{scroll-margin-top:calc(var(--top) + 12px);margin:22px 0}
.section .section{margin-left:0;padding-left:14px;border-left:1px solid var(--line-soft)}
.section-head{display:flex;justify-content:space-between;align-items:center;gap:12px}
.section h2{margin:0;font:600 17px/1.35 var(--ui);letter-spacing:-.01em}
.section .section h2{font-size:14.5px;color:var(--muted)}
.section-actions,.fragment-actions{display:flex;align-items:center;gap:2px}
.section-actions>.diff-button,.section-actions>.permalink,.fragment-actions>.diff-button,.fragment-actions>.landmark-menu,.fragment-actions>.permalink{opacity:0;transition:opacity .12s}
.section:hover>.section-actions>.diff-button,.section:hover>.section-actions>.permalink,.section:hover>.section-head>.section-actions>.diff-button,.section:hover>.section-head>.section-actions>.permalink,.section-head:focus-within>.section-actions>.diff-button,.section-head:focus-within>.section-actions>.permalink,.fragment:hover>.fragment-head>.fragment-actions>.diff-button,.fragment:hover>.fragment-head>.fragment-actions>.landmark-menu,.fragment:hover>.fragment-head>.fragment-actions>.permalink,.fragment-head:focus-within>.fragment-actions>.diff-button,.fragment-head:focus-within>.fragment-actions>.landmark-menu,.fragment-head:focus-within>.fragment-actions>.permalink{opacity:1}
.review-controls{position:relative;display:flex;align-items:center;gap:3px;opacity:.5;transition:opacity .14s}
section:hover>.section-actions .review-controls,.section:hover>.section-head>.section-actions .review-controls,.fragment:hover>.fragment-head>.fragment-actions .review-controls,.review-controls:focus-within{opacity:1}
.review-identities{display:inline-flex;align-items:center;gap:4px;max-width:min(520px,42vw);overflow-x:auto;scrollbar-width:thin}
.review-identity{display:inline-flex;align-items:center;gap:4px;flex:0 0 auto;max-width:260px;padding:2px 6px;border:1px solid var(--line-soft);border-radius:99px;background:var(--bg-subtle);color:var(--muted);font:10.5px var(--ui)}
.review-identity.approved{border-color:color-mix(in srgb,var(--green) 32%,var(--line-soft));background:var(--approve-bg)}
.review-identity.rejected{border-color:color-mix(in srgb,var(--red) 32%,var(--line-soft));background:var(--reject-bg)}
.review-identity .reviewer-kind{font-weight:700;text-transform:uppercase;letter-spacing:.04em;color:var(--fg)}
.review-identity.ai .reviewer-kind{color:var(--accent)}
.review-identity .reviewer-author,.review-identity .reviewer-ai{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.review-identity .reviewer-ai{max-width:140px;color:var(--faint);font-family:var(--mono)}
.review-decision-note{max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding-right:4px;color:var(--muted);font:11px var(--ui)}
.review-decision-note[hidden]{display:none}
.review-decision-group{display:inline-flex;align-items:center;gap:1px;padding:2px;border:1px solid var(--line-soft);border-radius:7px;background:var(--bg-subtle)}
.review-decision{position:relative;display:grid;place-items:center;width:25px;height:23px;padding:0;border:0;border-radius:5px}
.review-decision .i{width:15px;height:15px}
.review-decision.approve{background:var(--approve-bg);color:var(--approve-ink)}
.review-decision.reject{background:var(--reject-bg);color:var(--reject-ink)}
.review-decision.approve:hover{background:var(--approve-hover)}
.review-decision.reject:hover{background:var(--reject-hover)}
.review-decision[aria-pressed=true].approve{background:var(--approve-hover);color:var(--green)}
.review-decision[aria-pressed=true].reject{background:var(--reject-hover);color:var(--red)}
.review-icon-filled{display:none}
.review-decision[aria-pressed=true] .review-icon-outline{display:none}
.review-decision[aria-pressed=true] .review-icon-filled{display:block}
.review-decision-tooltip{position:absolute;z-index:29;top:calc(100% + 8px);right:0;display:none;width:max-content;max-width:min(360px,80vw);padding:8px 10px;border:1px solid #ffffff20;border-radius:6px;background:#24292f;color:#fff;box-shadow:var(--shadow);text-align:left;font:12px/1.4 var(--ui);pointer-events:none}
.review-decision[aria-pressed=true]:hover .review-decision-tooltip,.review-decision[aria-pressed=true]:focus-visible .review-decision-tooltip{display:block}
.review-decision-tooltip-head{display:flex;align-items:baseline;justify-content:space-between;gap:18px}
.review-decision-tooltip-head strong{font-weight:600;white-space:nowrap}
.review-decision-tooltip-head span{color:#ffffffb8;font-size:11px}
.review-decision-tooltip-head span[hidden],.review-decision-tooltip-note[hidden]{display:none}
.review-decision-tooltip-note{display:block;margin-top:5px;padding-top:5px;border-top:1px solid #ffffff24;white-space:pre-wrap;overflow-wrap:anywhere;color:#ffffffeb}
.review-decision-tooltip-action{display:block;margin-top:5px;color:#ffffff8f;font-size:10.5px}
.review-comment{color:var(--faint)}
.section-actions>.review-comment{opacity:.5;transition:opacity .14s}
.section:hover>.section-head>.section-actions>.review-comment,.section-head:focus-within>.section-actions>.review-comment{opacity:1}
.review-controls.decision-changed .review-decision[aria-pressed=true]{animation:review-pop .5s cubic-bezier(.2,.85,.25,1.35)}
.review-decision-compose{position:absolute;z-index:28;top:calc(100% + 6px);right:0;width:min(340px,80vw);padding:8px;border:1px solid var(--line);border-radius:8px;background:var(--bg);box-shadow:var(--shadow);opacity:0;transform:translateY(-4px);pointer-events:none;transition:opacity .14s ease,transform .14s ease}
.review-decision-compose[hidden]{display:none}
.review-decision-compose.open{opacity:1;transform:none;pointer-events:auto}
.review-decision-compose textarea{display:block;width:100%;min-height:58px;padding:6px 8px;border:1px solid var(--line);border-radius:var(--radius);resize:vertical;font:12.5px var(--ui)}
.review-decision-compose>div{display:flex;align-items:center;justify-content:flex-end;gap:4px;margin-top:6px}
.review-decision-compose .btn-primary{padding:4px 10px}
@keyframes review-pop{0%{transform:scale(.75)}55%{transform:scale(1.18)}100%{transform:scale(1)}}
.fragment{position:relative;scroll-margin-top:calc(var(--top) + 12px);margin:14px 0;padding:0 0 0 12px;border-left:2px solid transparent;outline:0}
.fragment.active-fragment{border-left-color:var(--active-fragment-line)}
.fragment-head{position:relative;z-index:20;display:flex;justify-content:flex-end;gap:8px;align-items:center;min-height:22px}
.annotation-tools-toggle{color:var(--faint)}
.annotation-tools-toggle[aria-expanded=true]{background:var(--accent-soft);color:var(--accent)}
.fragment-stage{position:relative;min-height:40px}
.fragment-frame{display:block;width:100%;min-height:380px;border:1px solid var(--line-soft);border-radius:var(--radius);background:var(--bg)}
.fragment-image{display:block;max-width:100%;height:auto}
.fragment-markdown{font:14px/1.65 var(--ui);color:var(--ink)}
.fragment-markdown>:first-child{margin-top:0}
.fragment-markdown h1,.fragment-markdown h2,.fragment-markdown h3,.fragment-markdown h4{margin:1.5em 0 .4em;font-weight:600;letter-spacing:-.01em;line-height:1.3}
.fragment-markdown h1{font-size:18px}
.fragment-markdown h2{font-size:16px}
.fragment-markdown h3{font-size:14px}
.fragment-markdown h4{font-size:13px;color:var(--muted)}
.fragment-markdown code{padding:.15em .35em;border-radius:4px;background:var(--bg-inset);font:12px var(--mono)}
.fragment-markdown pre,.plain{overflow:auto;padding:10px 12px;border:1px solid var(--line-soft);border-radius:var(--radius);background:var(--bg-subtle);font:12px/1.5 var(--mono)}
.fragment-markdown pre code{padding:0;background:transparent}
.fragment-markdown blockquote{margin:1em 0;padding-left:12px;border-left:2px solid var(--line);color:var(--muted)}
.fragment-markdown table{border-collapse:collapse;font-size:12.5px}
.fragment-markdown th,.fragment-markdown td{padding:5px 9px;border:1px solid var(--line-soft);text-align:left}
.fragment-markdown th{background:var(--bg-subtle)}
.fragment-markdown .footnote-ref{display:inline-flex;align-items:center;justify-content:center;min-width:1.25em;height:1.25em;margin:0 .08em;padding:0 .25em;border-radius:999px;background:var(--bg-inset);color:var(--muted);font:600 10px/1 var(--mono);text-decoration:none;vertical-align:super}
.fragment-markdown .footnote-ref:hover,.fragment-markdown .footnote-ref:focus-visible{background:var(--footnote-hover-bg);color:var(--accent)}
.fragment-markdown .footnote-ref.diff-citation{background:var(--citation-bg);color:var(--accent);cursor:pointer}
.fragment-markdown .footnotes{margin-top:24px;color:var(--muted);font-size:12.5px}
.fragment-markdown .footnotes hr{height:1px;margin:0 0 10px;border:0;background:var(--line-soft)}
.fragment-markdown .footnotes ol{margin:0;padding-left:24px}
.fragment-markdown .footnotes li{padding:2px 0 2px 4px}
.fragment-markdown .footnotes p{margin:.35em 0}
.fragment-markdown .footnote-backref{color:var(--muted);text-decoration:none}
.fragment-markdown .footnotes .content-landmark-text{background:var(--landmark-bg)}

/* Quiet controls --------------------------------------------------------- */
.btn,button{font:12px var(--ui);border:1px solid transparent;border-radius:var(--radius);padding:4px 9px;background:var(--bg-inset);color:var(--ink)}
.btn:hover,button:hover{background:var(--button-hover)}
.btn-primary{background:var(--primary-bg);border-color:var(--primary-bg);color:var(--primary-ink)}
.btn-primary:hover{background:var(--primary-hover)}
.btn-subtle{background:transparent;color:var(--muted)}
.icon-button{display:inline-grid;place-items:center;width:24px;height:24px;padding:0;border:0;background:transparent;color:var(--muted)}
.icon-button:hover{background:var(--bg-inset);color:var(--ink)}
.icon-button[aria-pressed=true]{background:var(--accent-soft);color:var(--accent)}
.diff-button{display:inline-flex;align-items:center;gap:4px;width:auto;padding:0 6px;font:11px var(--mono)}
.diff-counts{display:inline-flex;gap:5px;font-weight:600}
.diff-counts .add{color:var(--green)}
.diff-counts .del{color:var(--red)}
.permalink{position:relative;display:inline-grid;place-items:center;width:24px;height:24px;padding:0;border:0;background:transparent;color:var(--faint)}
.permalink:hover{background:var(--bg-inset);color:var(--accent)}
.permalink.copied:after{position:absolute;right:0;bottom:calc(100% + 4px);z-index:5;content:'Copied';padding:2px 6px;border-radius:4px;background:var(--copied-bg);color:var(--copied-ink);font:11px var(--ui);white-space:nowrap}
.fragment-heading{display:flex;align-items:center;gap:4px;scroll-margin-top:calc(var(--top) + 12px)}
.fragment-heading .heading-permalink{display:inline-grid;place-items:center;width:22px;height:20px;opacity:0;color:var(--faint);text-decoration:none}
.fragment-heading:hover .heading-permalink,.fragment-heading:focus-within .heading-permalink,.heading-permalink:focus-visible{opacity:1}

/* Landmarks -------------------------------------------------------------- */
.landmark-target{position:absolute;top:0;left:0;width:1px;height:1px;scroll-margin-top:calc(var(--top) + 12px)}
.landmark-menu{position:relative}
.landmark-menu>summary{display:grid;place-items:center;width:24px;height:24px;list-style:none;border-radius:var(--radius);color:var(--muted);cursor:pointer}
.landmark-menu>summary:hover{background:var(--bg-inset);color:var(--ink)}
.landmark-menu>summary::-webkit-details-marker{display:none}
.landmark-list{position:absolute;z-index:20;right:0;top:calc(100% + 4px);width:min(280px,75vw);padding:4px;background:var(--bg);border:1px solid var(--line);border-radius:var(--radius);box-shadow:var(--shadow)}
.landmark-list>div{display:flex;align-items:center;gap:2px;border-radius:4px}
.landmark-list>div:hover{background:var(--bg-subtle)}
.landmark-list a:first-child{min-width:0;flex:1;padding:5px 7px;color:var(--ink);text-decoration:none;font-size:12.5px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.content-landmark-region{pointer-events:none;fill:transparent;stroke:transparent;stroke-width:5;vector-effect:non-scaling-stroke}
.content-landmark-region.active{fill:#f2bd4b33;stroke:#d39418}
.content-landmark-active{background:#f2bd4b40;outline:2px solid #d39418}
.landmark-affordance{display:inline-flex;align-items:center;gap:1px;padding:1px;border-radius:var(--radius);background:var(--landmark-affordance-bg);border:1px solid var(--line);box-shadow:var(--shadow);opacity:0;transition:opacity .12s}
.fragment-heading:hover>.landmark-affordance,.fragment-heading:focus-within>.landmark-affordance,.content-landmark-text:hover>.landmark-affordance,.content-landmark-text:focus-within>.landmark-affordance{opacity:1}
.content-landmark-text{position:relative;display:inline;border-radius:3px;background:transparent;color:inherit}
.content-landmark-text>.landmark-affordance{position:absolute;z-index:4;left:calc(100% + 4px);top:50%;transform:translateY(-50%);white-space:nowrap}
.content-landmark-text.content-landmark-active{background:#f2bd4b40;outline:2px solid #d39418}
.landmark-hotspot{position:absolute;z-index:3;border:1px solid transparent;border-radius:4px;pointer-events:auto}
.landmark-hotspot>.landmark-affordance{position:absolute;right:3px;top:3px}
.landmark-hotspot:hover,.landmark-hotspot:focus-within,.landmark-hotspot.active{border-color:#d39418;background:#f2bd4b1a}
.landmark-hotspot:hover>.landmark-affordance,.landmark-hotspot:focus-within>.landmark-affordance,.landmark-hotspot.active>.landmark-affordance{opacity:1}

/* Annotation overlay ----------------------------------------------------- */
.review-overlay{position:absolute;inset:0;width:100%;height:100%;pointer-events:none;overflow:visible}
.review-overlay.drawing{pointer-events:auto;cursor:crosshair;background:#0969da08}
.annotation{stroke-width:5;vector-effect:non-scaling-stroke}
.annotation.path{fill:none}
.annotation.selectable{pointer-events:visiblePainted;cursor:grab}
.annotation.selectable:active{cursor:grabbing}
.annotation.selected{stroke-width:8;stroke-dasharray:10 7;filter:drop-shadow(0 0 4px #fff)}
.annotation-resize-handle{pointer-events:all;cursor:nwse-resize;fill:#fff;stroke:var(--ink);stroke-width:3;vector-effect:non-scaling-stroke}
.pending{fill-opacity:.22}
.pending.path{fill:none}
.annotation-revealed .annotation,.annotation-revealed.sticky-note{filter:drop-shadow(0 0 3px var(--ink))}
mark.annotation-revealed{box-shadow:0 0 0 2px var(--ink)}

/* Annotation comment bubbles --------------------------------------------- */
/* A comment drawn onto the content stays with its mark: a compact bubble at
   the mark's top-right corner, opening its thread on hover or focus. Comments
   on a whole fragment, section, or chapter keep their list below the content. */
.annotation-bubble{position:absolute;z-index:16;left:0;top:0;transform:translate(-30%,-70%);line-height:0}
.review-overlay.drawing~.annotation-bubble{pointer-events:none}
.annotation-bubble.resolved{opacity:.55}
.annotation-bubble.resolved:hover,.annotation-bubble.open{opacity:1}
.annotation-bubble-toggle{display:inline-flex;align-items:center;gap:3px;height:21px;padding:0 6px;border:1px solid var(--line);border-radius:11px;background:var(--bg);color:var(--muted);font:11px/1 var(--mono);box-shadow:var(--shadow)}
.annotation-bubble-toggle .i{width:13px;height:13px}
.annotation-bubble-toggle:hover,.annotation-bubble.open>.annotation-bubble-toggle{background:var(--bg-inset);color:var(--ink);border-color:var(--ink)}
.annotation-bubble-panel{position:absolute;z-index:17;left:0;top:calc(100% + 5px);width:min(360px,72vw);max-height:min(340px,60vh);overflow:auto;padding:2px 10px 8px;border:1px solid var(--line);border-radius:var(--radius);background:var(--bg);box-shadow:var(--shadow);line-height:1.55;text-align:left;cursor:auto}
.annotation-bubble-panel[hidden]{display:none}
.annotation-bubble-panel.flip-y{top:auto;bottom:calc(100% + 5px)}
.annotation-bubble-panel .threads{margin-top:0}
.annotation-bubble-panel .thread{margin:8px 0 0}

/* Threads ---------------------------------------------------------------- */
.threads{margin-top:10px}
.thread{scroll-margin-top:calc(var(--top) + 12px);margin:8px 0;padding:8px 10px;border:1px solid var(--line-soft);border-left:2px solid var(--amber);border-radius:var(--radius);background:var(--bg-subtle)}
.thread.suggestion{border-left-color:var(--accent)}
.thread.resolved{opacity:.6;border-left-color:var(--faint)}
.thread-meta{display:flex;align-items:center;gap:5px;color:var(--faint);font:11px var(--mono)}
.thread-meta .permalink{margin-left:auto;width:20px;height:20px}
.suggestion-code{display:block;margin:7px 0;padding:8px 10px;border-radius:var(--radius);background:var(--add-bg);color:var(--suggestion-ink);white-space:pre-wrap;font:12px var(--mono)}
.message{scroll-margin-top:calc(var(--top) + 12px);margin:7px 0;padding-top:6px;border-top:1px solid var(--line-soft)}
.message-fragment{font-size:12.5px}
.message-fragment iframe{min-height:220px}
.reply{display:grid;grid-template-columns:1fr auto;gap:5px;margin-top:7px}
.reply input:not([type=hidden]),.dialog-form input,.dialog-form textarea,.diff-compose input,.diff-compose textarea{width:100%;padding:5px 8px;border:1px solid var(--line);border-radius:var(--radius);background:var(--bg)}
.reply input[type=file],.dialog-form input[type=file],.diff-compose input[type=file]{border:0;padding:0;font-size:11px;color:var(--muted)}
.thread-state{margin-top:5px}
.text-anchor{display:block;margin:5px 0;padding:2px 6px;border-radius:3px;background:var(--text-anchor-bg);font-size:12px}

/* Annotation toolbox ----------------------------------------------------- */
.annotation-toolbox{position:absolute;z-index:50;top:calc(100% + 5px);right:0;display:flex;align-items:center;gap:1px;max-width:min(620px,calc(100vw - 32px));padding:4px;background:var(--bg);color:var(--ink);border:1px solid var(--line);border-radius:9px;box-shadow:var(--shadow)}
.annotation-toolbox[hidden]{display:none}
.annotation-toolbox button{display:grid;place-items:center;width:28px;height:28px;padding:0;border:0;border-radius:var(--radius);background:transparent;color:var(--muted)}
.annotation-toolbox button:hover{background:var(--bg-inset);color:var(--ink)}
.annotation-toolbox button[aria-pressed=true]{background:var(--accent-soft);color:var(--accent)}
.annotation-toolbox button:disabled{opacity:.35;cursor:not-allowed;background:transparent}
.tool-divider{width:1px;height:18px;margin:0 3px;background:var(--line)}
.annotation-color{display:grid;place-items:center;width:28px;height:28px;border-radius:var(--radius)}
.annotation-color:hover{background:var(--bg-inset)}
.annotation-color input{width:18px;height:18px;padding:0;border:0;background:transparent;cursor:pointer}
.annotation-color input::-webkit-color-swatch-wrapper{padding:0}
.annotation-color input::-webkit-color-swatch{border:1px solid var(--line);border-radius:4px}
.annotation-selection-tools{display:flex;align-items:center;gap:1px;padding-left:3px;border-left:1px solid var(--line)}
.annotation-selection-tools[hidden]{display:none}
.annotation-selection-tools button:hover{color:var(--red)}
.tool-target{max-width:150px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding:0 8px;color:var(--faint);font:11px var(--mono)}

/* Dialogs, drawers, composers -------------------------------------------- */
.dialog{width:min(480px,calc(100vw - 32px));border:1px solid var(--line);border-radius:8px;padding:0;background:var(--bg);color:var(--ink);box-shadow:0 16px 48px #1f232833}
.dialog::backdrop{background:#1f232866}
.dialog-head{display:flex;align-items:center;gap:10px;padding:10px 12px;border-bottom:1px solid var(--line-soft)}
.dialog-head h2{margin:0;font:600 13px var(--ui)}
.dialog-head .icon-button{margin-left:auto}
.dialog-form{padding:14px}
.dialog-form textarea{min-height:92px;margin-bottom:12px;font:13px/1.5 var(--ui);resize:vertical}
.dialog-actions{display:flex;align-items:center;gap:14px;padding-top:12px;border-top:1px solid var(--line-soft)}
.dialog-actions input[type=file]{flex:1 1 240px;min-width:0;width:auto;color:var(--muted);font:11px var(--ui)}
.dialog-actions input[type=file]::file-selector-button{margin-right:9px;padding:5px 9px;border:1px solid var(--line);border-radius:var(--radius);background:var(--bg-inset);color:var(--ink);font:12px var(--ui);cursor:pointer}
.dialog-actions input[type=file]::file-selector-button:hover{background:var(--button-hover)}
.dialog-actions .btn-primary{flex:none;min-width:88px;padding:7px 14px}
.annotation-compose{position:fixed;z-index:90;right:18px;bottom:18px;width:min(440px,calc(100vw - 36px));display:none;box-shadow:var(--shadow)}
.annotation-compose.open{display:block}
.annotation-compose.anchored{position:absolute;right:auto;bottom:auto}
.diff-drawer{position:fixed;z-index:80;inset:var(--top) 0 0 auto;width:min(1100px,92vw);background:var(--bg);border-left:1px solid var(--line);box-shadow:-12px 0 40px #1f23281f;transform:translateX(105%);transition:transform .2s ease;display:flex;flex-direction:column}
.diff-drawer.open{transform:none}
.drawer-backdrop{position:fixed;z-index:70;inset:var(--top) 0 0;background:#1f232833;display:none}
.drawer-backdrop.open{display:block}
.drawer-head{min-height:38px;display:flex;align-items:center;gap:8px;padding:5px 10px;border-bottom:1px solid var(--line);background:var(--bg)}
.drawer-head strong{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font:600 12.5px var(--ui)}
.drawer-head .icon-button{margin-left:auto}
.drawer-body{overflow:auto;padding-bottom:60px;background:var(--bg)}
.diff-drawer[data-drawer-mode=fragment] .drawer-body{padding:10px clamp(16px,4vw,54px) 72px}
.diff-drawer[data-drawer-mode=fragment] .fragment{max-width:980px;margin:0 auto;padding-left:0}
.diff-drawer[data-drawer-mode=fragment] .fragment.active-fragment{border-left-color:transparent}
.diff-drawer[data-drawer-mode=activity]{width:min(620px,92vw)}
.diff-drawer[data-drawer-mode=activity] .drawer-body{padding-bottom:0}
.activity-drawer-surface{min-height:100%}
.drawer-body .file-head{top:0}
.drawer-body .diff-column-head{top:38px}

/* Linked code (drawer contents) ------------------------------------------ */
.attached-code-summary{display:flex;align-items:center;gap:10px;padding:7px 12px;border-bottom:1px solid var(--line-soft);background:var(--bg-subtle);color:var(--muted);font:11px var(--mono)}
.attached-code-scope{margin-left:auto;color:var(--faint);font-family:var(--ui)}
.attached-file-list{padding:8px 10px}
.attached-file{margin:6px 0;border:1px solid var(--line);border-radius:var(--radius);overflow:hidden;background:var(--bg)}
.attached-file>summary{display:flex;align-items:center;gap:8px;padding:7px 10px;list-style:none;cursor:pointer}
.attached-file>summary::-webkit-details-marker{display:none}
.attached-file>summary:hover{background:var(--bg-subtle)}
.attached-file>summary .twisty{width:12px;height:12px;flex:none;color:var(--faint);transition:transform .12s}
.attached-file[open]>summary .twisty{transform:rotate(90deg)}
.attached-file-main{display:flex;min-width:0;flex:1;flex-direction:column}
.attached-file-main code{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font:600 12px var(--mono)}
.attached-file-note{overflow:hidden;color:var(--muted);font-size:12px;text-overflow:ellipsis;white-space:nowrap}
.attached-file-note.missing{color:var(--amber)}
.attached-file-counts{margin-left:auto;color:var(--faint);font:11px var(--mono);white-space:nowrap}
.attached-file>.diff-surface{border-top:1px solid var(--line-soft);overflow:auto}
.review-file-diff-actions{display:flex;align-items:center;gap:8px;padding:5px 10px;background:var(--bg-subtle);color:var(--faint);font:11px var(--mono);border-bottom:1px solid var(--line-soft)}
.review-file-diff-actions a{display:inline-flex;align-items:center;gap:4px;margin-left:auto;color:var(--accent);text-decoration:none}
.attached-file .diff-column-head{position:static!important;top:auto!important}
.attached-file .diff-row.linked-evidence{box-shadow:inset 3px 0 var(--accent)}
.attached-file .diff-row.linked-evidence .line-no:first-child{color:var(--accent)}
.diff-surface.loading [data-file-diff-rows]{opacity:.55}
.manifest-file-diff.loading [data-manifest-diff-rows]{opacity:.55}
.diff-placeholder{margin:0;padding:8px 10px;color:var(--faint);font:11px var(--mono)}

/* Code workspace --------------------------------------------------------- */
.code-toolbar{position:sticky;top:var(--top);z-index:6;display:flex;align-items:center;gap:2px;padding:5px clamp(8px,1.4vw,14px);background:var(--bg);border-bottom:1px solid var(--line)}
.code-toolbar .spacer{flex:1}
.code-toolbar .metric{color:var(--faint);font:11px var(--mono);padding-right:6px}
.code-workspace{display:grid;grid-template-columns:minmax(0,1fr) minmax(240px,300px);gap:0;align-items:start}
.code-workspace.related-hidden{grid-template-columns:minmax(0,1fr) 0}
.code-workspace.related-hidden .related-saga{visibility:hidden;overflow:hidden;padding:0;border:0}
.code-main{min-width:0}
.related-saga{position:sticky;top:calc(var(--top) + 35px);max-height:calc(100vh - var(--top) - 35px);overflow:auto;padding:10px 12px;border-left:1px solid var(--line)}
.related-head{display:flex;align-items:center;gap:6px;margin-bottom:6px}
.related-head h2{margin:0;font:600 11px var(--ui);letter-spacing:.02em;color:var(--muted)}
.related-head .icon-button{margin-left:auto}
.related-chapter{border-top:1px solid var(--line-soft);padding-top:8px;margin-top:8px}
.related-chapter:first-of-type{border-top:0;margin-top:0;padding-top:0}
.related-chapter-link{display:block;color:var(--muted);text-decoration:none;font:11px var(--mono)}
.related-chapter h3.related-chapter-link{margin:0;text-transform:uppercase;letter-spacing:.035em}
.related-chapter-link:hover{color:var(--accent)}
.related-group-items{display:grid;gap:10px}
.related-fragment{display:block;margin-top:6px;padding:6px 8px;border-radius:var(--radius);background:var(--bg-subtle);color:var(--ink);text-decoration:none}
.related-fragment:hover{background:var(--sel)}
.related-fragment strong{display:block;font-size:12.5px}
.related-fragment span{display:block;margin-top:2px;color:var(--muted);font-size:11.5px;line-height:1.45}
.related-slide{display:block;margin-top:7px;color:var(--ink);text-decoration:none}
.related-slide-preview{display:block;aspect-ratio:16/9;overflow:hidden;border:2px solid var(--line);border-radius:5px;background:#fff;box-shadow:0 1px 2px #1f23281f;transition:border-color .12s,box-shadow .12s}
.related-slide-preview iframe,.related-slide-preview img{display:block;width:100%;height:100%;border:0;object-fit:contain;pointer-events:none}
.related-slide-caption{display:flex;align-items:center;gap:5px;margin-top:5px}
.related-slide-caption strong{min-width:0;flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12.5px}
.related-slide>small{display:block;margin-top:2px;color:var(--muted);font:10.5px var(--mono)}
.related-slide:hover .related-slide-preview{border-color:var(--accent);box-shadow:0 0 0 1px var(--accent)}
.related-saga>p{color:var(--muted);font-size:12px}

/* Diff surface ----------------------------------------------------------- */
.file-diff{scroll-margin-top:calc(var(--top) + 36px);margin:0;background:var(--code-bg)}
.file-head{position:sticky;top:calc(var(--top) + 35px);z-index:5;display:flex;align-items:center;gap:8px;padding:6px clamp(8px,1.4vw,14px);background:var(--bg-subtle);border-bottom:1px solid var(--line)}
.file-head code{font:600 12.5px var(--mono);overflow-wrap:anywhere}
.file-head .counts{margin-left:auto;font:11px var(--mono);color:var(--faint)}
.reviewed-badge{display:inline-flex;align-items:center;gap:3px;color:var(--green);font:11px var(--mono)}
.file-review-menu{position:relative}
.file-review-menu summary{display:grid;place-items:center;width:24px;height:24px;list-style:none;cursor:pointer;border-radius:var(--radius);color:var(--muted)}
.file-review-menu summary:hover{background:var(--bg-inset);color:var(--ink)}
.file-review-menu[open] summary{background:var(--accent-soft);color:var(--accent)}
.file-review-menu summary::-webkit-details-marker{display:none}
.file-review{position:absolute;right:0;top:calc(100% + 5px);z-index:8;width:210px;padding:10px;background:var(--bg);border:1px solid var(--line);border-radius:var(--radius);box-shadow:var(--shadow)}
.file-review p{margin:0 0 8px;color:var(--muted);font-size:12px}
.diff-surface{overflow:auto;background:var(--code-bg)}
.diff-column-head{display:none;grid-template-columns:1fr 1fr;position:sticky;top:calc(var(--top) + 68px);z-index:4;min-width:640px;background:var(--bg-subtle);color:var(--muted);border-bottom:1px solid var(--line-soft);font:10px var(--mono);letter-spacing:.04em;text-transform:uppercase}
.diff-column-head span{padding:3px 10px}
.diff-lines{min-width:640px}
.diff-row{display:grid;grid-template-columns:46px 46px 18px minmax(240px,1fr) 58px;align-items:start;min-height:20px;font:12px/1.5 var(--mono);position:relative;background:var(--code-bg)}
.diff-row[hidden]{display:none}
.diff-row.context{color:var(--ink)}
.diff-row.new{background:var(--add-bg);box-shadow:inset 2px 0 var(--add-line)}
.diff-row.old{background:var(--del-bg);box-shadow:inset 2px 0 var(--del-line)}
.diff-row.event{background:var(--bg-subtle);color:var(--muted)}
.diff-row.selected{outline:2px solid var(--accent);outline-offset:-2px;z-index:1}
.line-no{display:block;padding:1px 8px;text-align:right;color:var(--faint);user-select:none;background:var(--code-gutter);border-right:1px solid var(--line-soft)}
.diff-row.new .line-no{background:var(--add-gutter)}
.diff-row.old .line-no{background:var(--del-gutter)}
.line-select{width:100%;height:100%;min-height:20px;padding:1px 8px;border:0;border-radius:0;background:var(--code-gutter);color:var(--faint);text-align:right;font:inherit;border-right:1px solid var(--line-soft)}
.diff-row.new .line-select{background:var(--add-gutter)}
.diff-row.old .line-select{background:var(--del-gutter)}
.line-select:hover,.line-select:focus-visible{background:var(--primary-bg);color:var(--primary-ink)}
.sign{padding:1px 4px;color:var(--muted);text-align:center;user-select:none}
.code-line{padding:1px 10px;white-space:pre;overflow:visible;tab-size:4}
.line-actions{display:flex;justify-content:flex-end;gap:1px;padding:0 4px;opacity:0}
.diff-row:hover .line-actions,.diff-row:focus-within .line-actions{opacity:1}
.line-actions .icon-button{width:20px;height:20px;background:var(--bg);border:1px solid var(--line)}
.diff-thread-wrap{grid-column:4/6;padding:0 10px 6px}
.context-expander{display:grid;grid-template-columns:auto minmax(180px,1fr) auto;align-items:stretch;width:100%;padding:0;background:var(--bg-inset);color:var(--accent);border-block:1px solid var(--line-soft);font:11px var(--mono)}
.context-expander button{min-height:26px;padding:3px 10px;border:0;border-radius:0;background:transparent;color:inherit;font:inherit}
.context-expander button:hover,.context-expander button:focus-visible{background:var(--accent-soft)}
.context-expander .context-expand-all{grid-column:2}
.context-expander button[data-context-expand=down]{grid-column:1;grid-row:1}
.context-expander button[data-context-expand=up]{grid-column:3;grid-row:1}
.diff-selection-toolbar{display:none;position:sticky;bottom:10px;z-index:6;align-items:center;gap:5px;width:max-content;max-width:calc(100% - 20px);margin:-40px 12px 10px auto;padding:4px 6px;border:1px solid var(--line);border-radius:8px;background:var(--bg);box-shadow:var(--shadow);font:11px var(--mono)}
.diff-selection-toolbar.open{display:flex}
.diff-surface[data-layout=split] .diff-column-head{display:grid}
.diff-surface[data-layout=split] .diff-lines{display:grid;grid-template-columns:minmax(320px,1fr) minmax(320px,1fr);align-items:stretch}
.diff-surface[data-layout=split] .diff-row{grid-template-columns:46px 46px 18px minmax(180px,1fr);grid-column:auto}
.diff-surface[data-layout=split] .diff-row.context,.diff-surface[data-layout=split] .diff-row.event,.diff-surface[data-layout=split] .context-expander{grid-column:1/-1}
.diff-surface[data-layout=split] .diff-row.old{grid-column:1}
.diff-surface[data-layout=split] .diff-row.new{grid-column:2}
.diff-surface[data-layout=split] .line-actions{grid-column:4;position:absolute;right:4px}
.diff-surface[data-layout=split] .diff-thread-wrap{grid-column:4}
.tok-keyword{color:var(--syntax-keyword)}
.tok-string{color:var(--syntax-string)}
.tok-number{color:var(--syntax-number)}
.tok-comment{color:var(--syntax-comment)}
.tok-type{color:var(--syntax-type)}
.tok-property{color:var(--syntax-property)}
.tok-punctuation{color:var(--syntax-punctuation)}
.diff-compose{position:fixed;z-index:100;right:18px;bottom:18px;width:min(460px,calc(100vw - 36px));padding:12px;background:var(--bg);border:1px solid var(--line);border-radius:8px;box-shadow:var(--shadow);display:none}
.diff-compose.open{display:block}
.diff-compose-head{display:flex;justify-content:space-between;align-items:center;margin-bottom:8px}
.diff-compose-head strong{font:600 12.5px var(--ui)}
.diff-compose-fields{display:grid;grid-template-columns:1fr auto;gap:6px}
.diff-compose textarea{min-height:56px}
.replacement{display:none;grid-column:1/-1}
.diff-compose.suggesting .replacement{display:block}

/* Deferred review surfaces ---------------------------------------------- */
.surface-placeholder{display:flex;min-height:240px;align-items:center;justify-content:center;flex-direction:column;gap:7px;padding:28px;color:var(--muted);text-align:center;font-size:12.5px}
.surface-placeholder strong{color:var(--ink);font-size:14px}
.surface-placeholder.compact{min-height:120px;padding:18px 8px}
.surface-placeholder.error{color:var(--red)}
.surface-placeholder.error strong{color:var(--red)}
.surface-placeholder .btn-primary{margin-top:6px}
.surface-spinner{width:18px;height:18px;border:2px solid var(--line);border-top-color:var(--accent);border-radius:50%;animation:surface-spin .8s linear infinite}
@keyframes surface-spin{to{transform:rotate(360deg)}}
[data-surface-next]{display:flex;margin:14px auto;padding:6px 12px;border:1px solid var(--line);border-radius:var(--radius);background:var(--bg);color:var(--accent);font:600 12px var(--ui);text-decoration:none}
[data-surface-next]:hover,[data-surface-next]:focus-visible{border-color:var(--accent);background:var(--accent-soft)}
[data-surface-next][aria-busy=true]{cursor:progress;opacity:.65}

/* Coverage view ---------------------------------------------------------- */
.manifest-view{position:fixed;z-index:20;inset:var(--top) 0 0;background:var(--bg);overflow:auto}
.manifest-wrap{width:min(1180px,100%);margin:auto;padding:0 clamp(12px,2.5vw,28px) 64px}
.manifest-tools{position:sticky;top:0;z-index:4;display:flex;gap:8px;align-items:center;padding:8px 0;background:var(--toolbar-bg);backdrop-filter:blur(6px);border-bottom:1px solid var(--line-soft)}
.manifest-modes{display:flex;gap:2px;padding:2px;border-radius:var(--radius);background:var(--bg-inset)}
.manifest-modes button{background:transparent;color:var(--muted);padding:3px 9px;font-size:12px}
.manifest-modes button[aria-pressed=true]{background:var(--bg);color:var(--ink);box-shadow:0 1px 2px #1f232826}
.manifest-tools .manifest-metric{color:var(--faint);font:11px var(--mono)}
.manifest-tools .tree-search{max-width:280px;margin-left:auto;flex:1}
.manifest-tools input{min-width:0;width:100%;height:26px;padding:0 8px 0 25px;border:1px solid var(--line);border-radius:var(--radius);background:var(--bg);font-size:12px}
.manifest-alert{display:flex;gap:9px;align-items:flex-start;margin:12px 0;padding:10px 12px;border:1px solid var(--danger-line);border-left:3px solid var(--red);border-radius:var(--radius);background:var(--danger-bg);font-size:12.5px}
.manifest-alert .i{color:var(--red);margin-top:1px}
.manifest-alert strong{display:block;margin-bottom:2px}
.manifest-panel{padding-top:6px}
.mtree{padding:2px 0}
.mtree details{display:block}
.mtree summary{display:flex;align-items:center;gap:6px;min-height:24px;padding:0 8px 0 calc(6px + var(--depth,0) * 13px);list-style:none;cursor:pointer;border-radius:var(--radius);color:var(--muted);font:12px var(--mono)}
.mtree summary::-webkit-details-marker{display:none}
.mtree summary:hover{background:var(--bg-subtle)}
.mtree .twisty{width:12px;height:12px;flex:none;color:var(--faint);transition:transform .12s}
.mtree details[open]>summary .twisty{transform:rotate(90deg)}
.manifest-file{border-bottom:1px solid var(--line-soft)}
.manifest-file>summary{color:var(--ink)}
.manifest-file>summary .mfile-name{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.manifest-file.has-gap>summary{background:var(--danger-bg)}
.manifest-file.has-gap>summary .mfile-name{color:var(--red)}
.manifest-file-stats{margin-left:auto;padding-left:10px;color:var(--faint);font:11px var(--mono);white-space:nowrap}
.manifest-file-stats .gap{color:var(--red);font-weight:600}
.manifest-file-detail{margin-left:calc(20px + var(--depth,0) * 13px);border-left:1px solid var(--line-soft)}
.manifest-file-diff{max-height:min(54vh,620px);overflow:auto;border-block:1px solid var(--line-soft)}
.manifest-file-diff .diff-lines{min-width:580px}
.manifest-file-diff .diff-row{grid-template-columns:46px 46px 18px minmax(240px,1fr) 0}
.manifest-map-heading{padding:5px 10px;border-bottom:1px solid var(--line-soft);background:var(--bg-subtle);color:var(--faint);font:600 10px var(--ui);letter-spacing:.045em;text-transform:uppercase}
.manifest-file-detail>.manifest-rows{padding-left:0}
.manifest-rows{padding-left:calc(20px + var(--depth,0) * 13px);border-left:0}
.manifest-row{display:grid;grid-template-columns:minmax(220px,.85fr) minmax(280px,1.15fr);border-top:1px solid var(--line-soft)}
.manifest-row.unmapped{background:var(--danger-bg)}
.manifest-range{display:grid;grid-template-columns:62px minmax(0,1fr);align-items:center;gap:8px;padding:5px 10px;color:var(--ink);text-decoration:none}
.manifest-range:hover{background:var(--sel)}
.manifest-range>span{color:var(--muted);font:11px var(--mono)}
.manifest-range small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--faint);font:11px var(--ui)}
.manifest-owners{display:flex;align-items:center;gap:5px;flex-wrap:wrap;padding:4px 10px;border-left:1px solid var(--line-soft)}
.manifest-owners>a{display:inline-flex;align-items:center;gap:5px;max-width:100%;padding:2px 7px;border:1px solid var(--line);border-radius:99px;background:var(--bg);color:var(--ink);text-decoration:none;font-size:11.5px}
.manifest-owners>a:hover{border-color:var(--accent);color:var(--accent)}
.manifest-owners>a .i{width:12px;height:12px;color:var(--faint)}
.manifest-owners>a strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-weight:500}
.manifest-owners>a small{color:var(--faint)}
.manifest-owners>a.manifest-slide-owner{display:grid;grid-template-columns:72px minmax(0,1fr);gap:7px;padding:3px;border-radius:5px}
.manifest-slide-preview,.manifest-target-slide-preview,.activity-slide-preview{display:block;aspect-ratio:16/9;overflow:hidden;border:1px solid var(--line);border-radius:3px;background:#fff}
.manifest-slide-preview iframe,.manifest-slide-preview img,.manifest-target-slide-preview iframe,.manifest-target-slide-preview img,.activity-slide-preview iframe,.activity-slide-preview img{display:block;width:100%;height:100%;border:0;object-fit:contain;pointer-events:none}
.manifest-slide-copy{display:grid;min-width:0;align-content:center}
.manifest-owners>a .manifest-slide-copy strong,.manifest-owners>a .manifest-slide-copy small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.manifest-owner-missing,.manifest-gap{color:var(--red);font-size:11.5px}
.manifest-target-title{display:flex;min-width:0;align-items:baseline;gap:7px}
.manifest-target-title strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font:500 12.5px var(--ui)}
.manifest-target-title small{color:var(--faint);font:11px var(--mono)}
.manifest-target{border-bottom:1px solid var(--line-soft)}
.manifest-target>summary{color:var(--ink)}
.manifest-target-slide-preview{width:64px;flex:none}
.manifest-target-files{padding-left:20px}
.manifest-target-file{border-top:1px solid var(--line-soft)}
.manifest-target-file>summary{padding-left:8px;color:var(--ink)}
.manifest-target-file>summary code{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font:11.5px var(--mono)}
.manifest-target-file-detail{margin-left:20px;border-left:1px solid var(--line-soft)}
.manifest-target-links{display:flex;align-items:center;gap:5px;min-height:30px;padding:4px 8px;border-top:1px solid var(--line-soft);color:var(--faint);font:11px var(--ui)}
.manifest-target-links>a{padding:2px 6px;border:1px solid var(--line);border-radius:99px;color:var(--muted);text-decoration:none;font:11px var(--mono)}
.manifest-target-links>a:hover{border-color:var(--accent);color:var(--accent)}
.manifest-target-links .manifest-full-diff{display:inline-flex;align-items:center;gap:4px;margin-left:auto;border:0;color:var(--accent);font-family:var(--ui)}
.manifest-target-links .manifest-full-diff .i{width:12px;height:12px}
.manifest-open-saga{display:inline-flex;align-items:center;gap:5px;padding:6px 10px;color:var(--accent);text-decoration:none;font-size:12px}
.manifest-empty,.manifest-filter-empty{padding:18px;color:var(--muted);text-align:center;font-size:12.5px}
.manifest-orphans{margin-top:20px;padding-top:14px;border-top:1px solid var(--line)}
.manifest-orphans h2{margin:0;font:600 13px var(--ui)}
.manifest-orphans>p{margin:2px 0 8px;color:var(--muted);font-size:12px}
.manifest-orphan{display:flex;align-items:center;gap:8px;margin-top:6px;padding:8px 10px;border:1px solid var(--danger-line);border-radius:var(--radius);background:var(--danger-bg);font-size:12.5px}
.manifest-orphan>.i{color:var(--red);flex:none}
.manifest-orphan strong,.manifest-orphan small{display:block}
.manifest-orphan small{color:var(--muted);font-size:11.5px}
.manifest-orphan a{margin-left:auto;color:var(--accent);text-decoration:none;white-space:nowrap}

/* Review activity -------------------------------------------------------- */
.activity-wrap{width:100%;padding:20px 16px 72px}
.activity-heading{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;padding-bottom:18px;border-bottom:1px solid var(--line)}
.activity-heading .eyebrow{margin:0 0 2px;color:var(--faint);font:600 10px var(--mono);letter-spacing:.08em;text-transform:uppercase}
.activity-heading h1{margin:0;font-size:25px;line-height:1.2;letter-spacing:-.025em}
.activity-heading p:not(.eyebrow){margin:5px 0 0;color:var(--muted);font-size:13px}
.activity-summary{flex:none;color:var(--faint);font:11px var(--mono);white-space:nowrap}
.activity-summary strong{color:var(--ink)}
.activity-toolbar{position:sticky;z-index:4;top:0;display:flex;gap:3px;padding:10px 0;background:var(--toolbar-bg);backdrop-filter:blur(6px)}
.activity-toolbar button{display:flex;align-items:center;gap:6px;padding:4px 10px;border:1px solid transparent;border-radius:99px;background:transparent;color:var(--muted);font-size:12px}
.activity-toolbar button:hover{background:var(--bg-subtle);color:var(--ink)}
.activity-toolbar button[aria-pressed=true]{border-color:var(--line);background:var(--bg-inset);color:var(--ink);font-weight:600}
.activity-toolbar button span{color:var(--faint);font:10px var(--mono)}
.activity-list{display:grid;gap:12px}
.activity-card{scroll-margin-top:calc(var(--top) + 52px);padding:14px 16px;border:1px solid var(--line);border-left:3px solid var(--accent-line);border-radius:8px;background:var(--bg);box-shadow:0 1px 2px #1f23280d}
.activity-card.thread.open{border-left-color:var(--amber)}
.activity-card.thread.resolved,.activity-card.decision.approved{border-left-color:var(--green)}
.activity-card.decision.rejected{border-left-color:var(--red)}
.activity-card-head{display:flex;align-items:flex-start;justify-content:space-between;gap:18px;padding-bottom:8px;border-bottom:1px solid var(--line-soft)}
.activity-card-title{display:flex;align-items:center;gap:7px;flex-wrap:wrap}
.activity-kind{color:var(--muted);font:600 11px var(--ui)}
.activity-state{padding:1px 7px;border-radius:99px;background:var(--bg-inset);color:var(--faint);font:600 10px var(--ui)}
.activity-state.open{background:var(--warning-bg);color:var(--amber)}
.activity-state.resolved,.activity-state.approved{background:var(--approve-bg);color:var(--green)}
.activity-state.rejected{background:var(--reject-bg);color:var(--red)}
.activity-target{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:baseline;gap:5px;max-width:56%;color:var(--ink);text-decoration:none;text-align:right}
.activity-target>span{color:var(--faint);font:10px var(--mono);text-transform:uppercase}
.activity-target strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px}
.activity-target small{grid-column:2;color:var(--faint);font-size:11px}
.activity-target .i{grid-column:3;grid-row:1/3;width:13px;height:13px;align-self:center;color:var(--accent)}
.activity-target:hover strong{color:var(--accent);text-decoration:underline}
.activity-target.activity-slide-target{grid-template-columns:112px minmax(0,1fr) auto;align-items:center;max-width:70%;text-align:left}
.activity-slide-copy{display:grid;min-width:0;gap:1px;color:inherit!important;font:inherit!important;text-transform:none!important}
.activity-slide-copy>span{color:var(--faint);font:10px var(--mono);text-transform:uppercase}
.activity-slide-copy strong,.activity-target .activity-slide-copy small{grid-column:auto;display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.activity-attribution{display:flex;align-items:center;gap:7px;margin-top:8px;color:var(--faint);font:10.5px var(--mono)}
.activity-attribution time{margin-left:auto}
.activity-body{margin-top:12px;font-size:13px}
.activity-body>:last-child{margin-bottom:0}
.activity-empty-note{margin:12px 0 0;color:var(--faint);font-style:italic}
.activity-conversation{display:grid;gap:9px;margin-top:12px}
.activity-message{overflow:hidden;border:1px solid var(--line);border-radius:7px;background:var(--bg);box-shadow:0 1px 2px #1f23280a}
.activity-message.reply{margin-left:18px;border-left:3px solid var(--accent-line)}
.activity-message-head{display:flex;align-items:baseline;justify-content:space-between;gap:12px;padding:6px 9px;border-bottom:1px solid var(--line-soft);background:var(--bg-subtle);color:var(--faint);font:10px var(--mono)}
.activity-message-head strong{overflow:hidden;color:var(--ink);font:600 11px var(--ui);text-overflow:ellipsis;white-space:nowrap}
.activity-message-head time{white-space:nowrap}
.activity-message-body{padding:8px 10px 10px}
.activity-message .message-fragment{margin:0}
.activity-message .fragment-markdown{font-size:13px}
.activity-message .fragment-markdown>:last-child{margin-bottom:0}
.activity-events{display:flex;gap:8px;flex-wrap:wrap;margin:8px 0 0;padding:8px 0 0;border-top:1px dashed var(--line-soft);list-style:none;color:var(--faint);font:10px var(--mono)}
.activity-events li{display:flex;align-items:center;gap:4px}
.activity-events li+li:before{content:"·";margin-right:4px}
.activity-event-state{color:var(--muted);font-weight:600;text-transform:capitalize}
.activity-filter-empty,.activity-empty{display:flex;min-height:180px;align-items:center;justify-content:center;flex-direction:column;gap:4px;color:var(--muted);text-align:center}
.activity-filter-empty[hidden]{display:none}
.activity-empty strong{color:var(--ink)}

/* Sticky notes ----------------------------------------------------------- */
.review-overlay.placing{cursor:copy}
.sticky-note{position:absolute;z-index:12;transform:translate(-50%,-50%);width:min(174px,58%);min-height:96px;display:flex;flex-direction:column;gap:4px;padding:10px 11px 8px;border:1px solid #1f232826;border-radius:2px;background:var(--note-color,#f2bd4b);color:#1b1a12;box-shadow:0 8px 18px #1f232833,0 1px 0 #ffffff5c inset;font:13px/1.45 var(--ui);cursor:grab;scroll-margin-top:90px;outline:0}
.sticky-note:active{cursor:grabbing}
.sticky-note:focus-visible,.sticky-note.selected,.sticky-note:target{box-shadow:0 0 0 3px var(--ink),0 8px 18px #1f232833}
.sticky-note.pending{box-shadow:0 0 0 2px var(--ink),0 8px 18px #1f232833}
.sticky-note-body{margin:0;overflow-wrap:anywhere;white-space:pre-wrap;max-height:184px;overflow:auto}
.sticky-note-text{width:100%;flex:1;min-height:72px;padding:0;border:0;background:transparent;color:inherit;font:inherit;resize:none;outline:0}
.sticky-note-actions{display:flex;align-items:center;justify-content:flex-end;gap:4px;margin-top:auto;opacity:0}
.sticky-note:hover .sticky-note-actions,.sticky-note:focus-within .sticky-note-actions{opacity:1}
.sticky-note-actions .permalink{width:26px;height:22px;color:#1b1a12b0}
.sticky-note-actions .permalink:hover,.sticky-note-actions .permalink:focus-visible{background:#ffffff70;color:#1b1a12}
.sticky-note-actions button{padding:4px 7px;font:600 10px var(--ui);background:#ffffff85;color:#1b1a12}
.note-anchor{margin:7px 0;padding:8px 10px;border-left:4px solid var(--note-color,#f2bd4b);background:#00000008;white-space:pre-wrap;overflow-wrap:anywhere;font:13px/1.5 var(--ui)}

/* Responsive ------------------------------------------------------------- */
@media(max-width:1050px){
.code-workspace{grid-template-columns:1fr}
.related-saga{position:static;max-height:none;border-left:0;border-top:1px solid var(--line)}
.code-workspace.related-hidden{grid-template-columns:1fr}
.diff-surface[data-layout=split] .diff-lines{display:block}
.diff-surface[data-layout=split] .diff-row{grid-template-columns:46px 46px 18px minmax(240px,1fr) 58px}
.diff-surface[data-layout=split] .diff-row,.diff-surface[data-layout=split] .diff-row.old,.diff-surface[data-layout=split] .diff-row.new{grid-column:auto}
.diff-surface[data-layout=split] .line-actions{grid-column:auto;position:static}
.diff-surface[data-layout=split] .diff-column-head{display:none}
}
@media(max-width:780px){
.sticky-note{width:min(150px,64%);min-height:84px;font-size:12px}
.annotation-bubble-panel{width:min(280px,80vw)}
.topbar{padding:0 8px;gap:6px}
.brand span{display:none}
.view-tab{padding:0 8px}
.review-progress{flex-basis:110px;gap:1px;padding-inline:2px}
.shell,.shell.code-mode{display:block}
.sidebar{position:static;height:auto;max-height:42vh;padding:8px}
.shell.code-mode .sidebar{position:fixed;z-index:25;top:var(--top);bottom:0;left:0;width:min(300px,86vw);max-height:none;height:auto;box-shadow:12px 0 30px #1f232826;transform:none;transition:transform .18s}
.shell.code-mode.tree-hidden .sidebar{transform:translateX(-105%);padding:8px}
.content{padding:16px 12px 90px}
.code-mode .content{padding:0 0 40px}
.chapter-body{padding-left:10px}
.chapter-review-directory{top:calc(var(--top) + 4px)}
.review-directory-item{grid-template-columns:10px minmax(90px,1fr) auto auto}
.review-directory-status{display:none}
.section .section{padding-left:8px}
.annotation-toolbox{right:0;overflow-x:auto;justify-content:flex-start}
.tool-target{display:none}
.reply,.diff-compose-fields{grid-template-columns:1fr}
.file-head{top:calc(var(--top) + 35px);flex-wrap:wrap}
.drawer-body .file-head{top:0}
.line-actions{opacity:1}
.code-toolbar{overflow-x:auto}
.diff-row{grid-template-columns:38px 38px 16px minmax(200px,1fr) 52px}
.diff-thread-wrap{grid-column:4/6}
.code-toolbar .metric{display:none}
.attached-file>summary{align-items:flex-start;flex-wrap:wrap}
.attached-file-note{white-space:normal}
.attached-file-counts{width:100%;margin-left:26px}
.manifest-tools{align-items:stretch;flex-direction:column}
.manifest-tools .tree-search{max-width:none;margin:0}
.manifest-row{grid-template-columns:1fr}
.manifest-owners{border-left:0;border-top:1px solid var(--line-soft)}
.manifest-target-files{padding-left:8px}
.manifest-target-links{align-items:flex-start;flex-wrap:wrap}
.manifest-target-links .manifest-full-diff{width:100%;margin-left:0}
.activity-wrap{padding:20px 12px 64px}
.activity-heading{align-items:flex-start;flex-direction:column;gap:8px}
.activity-card{padding:12px}
.activity-card-head{flex-direction:column;gap:7px}
.activity-target{max-width:100%;text-align:left}
.activity-attribution{align-items:flex-start;flex-direction:column;gap:2px}
.activity-attribution time{margin-left:0}
.diff-drawer{width:100vw}
}
`
