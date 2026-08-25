package server

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
--ui:-apple-system,BlinkMacSystemFont,"Segoe UI",system-ui,"Helvetica Neue",Arial,sans-serif;
--mono:ui-monospace,SFMono-Regular,"SF Mono",Menlo,Consolas,"Liberation Mono",monospace;
--top:44px;--shadow:0 6px 24px #1f232814,0 1px 3px #1f23281f;--radius:6px
}
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

/* Top bar ---------------------------------------------------------------- */
.topbar{position:sticky;top:0;z-index:30;height:var(--top);display:flex;align-items:center;gap:14px;padding:0 12px;background:var(--bg);border-bottom:1px solid var(--line)}
.brand{display:flex;align-items:center;gap:6px;color:var(--muted);font:600 11px var(--mono);letter-spacing:.04em}
.brand .i{width:14px;height:14px}
.view-tabs{display:flex;align-self:stretch;gap:2px}
.view-tab{display:flex;align-items:center;gap:6px;border:0;border-bottom:2px solid transparent;border-radius:0;padding:0 10px;background:transparent;color:var(--muted);font-size:12.5px}
.view-tab:hover{color:var(--ink);background:var(--bg-subtle)}
.view-tab.active{color:var(--ink);border-color:var(--accent);font-weight:600}
.top-meta{margin-left:auto;color:var(--faint);font:11px var(--mono)}
.top-meta[hidden]{display:none}
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
.file-tree{display:block}
.file-tree summary,.file-tree a{display:flex;align-items:center;gap:6px;min-height:24px;padding:0 6px 0 calc(4px + var(--depth,0) * 12px);border-radius:var(--radius);color:var(--ink);text-decoration:none;font:12px var(--mono);white-space:nowrap}
.file-tree summary{list-style:none;cursor:pointer;color:var(--muted)}
.file-tree summary::-webkit-details-marker{display:none}
.file-tree summary:hover,.file-tree a:hover{background:var(--bg-inset)}
.file-tree .twisty{width:12px;height:12px;flex:none;color:var(--faint);transition:transform .12s ease}
.file-tree details[open]>summary .twisty{transform:rotate(90deg)}
.file-tree .tree-name{min-width:0;overflow:hidden;text-overflow:ellipsis}
.file-tree .selected{background:var(--sel);box-shadow:inset 2px 0 var(--accent);color:var(--accent);font-weight:600}
.file-tree .tree-state{display:grid;place-items:center;width:14px;flex:none;color:transparent}
.file-tree .tree-state .i{width:12px;height:12px}
.file-tree .reviewed .tree-state{color:var(--green)}
.file-tree .reviewed .tree-name{color:var(--muted)}
.file-tree .counts{margin-left:auto;padding-left:8px;color:var(--faint);font:11px var(--mono)}
.file-tree .counts .add{color:var(--green)}
.file-tree .counts .del{color:var(--red)}

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
.alert{display:flex;gap:9px;align-items:flex-start;margin:0 0 20px;padding:10px 12px;border:1px solid #e0c98a;border-left:3px solid var(--amber);border-radius:var(--radius);background:#fff8e6;font-size:12.5px}
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
.chapter-review-directory{position:sticky;top:calc(var(--top) + 8px);z-index:18;margin:4px 0 16px;padding:0;border:1px solid var(--line);border-radius:9px;background:#ffffffed;box-shadow:0 6px 20px #1f23280d;backdrop-filter:blur(8px)}
.chapter-review-directory>summary{display:flex;align-items:center;justify-content:space-between;gap:12px;min-height:36px;padding:6px 10px;cursor:pointer;list-style:none;color:var(--ink);font:600 12px var(--ui)}
.chapter-review-directory>summary::-webkit-details-marker{display:none}
.review-directory-heading{display:flex;align-items:center;gap:7px}
.review-directory-heading .i{width:14px;height:14px;color:var(--accent)}
.review-directory-summary{color:var(--faint);font:11px var(--mono)}
.review-directory-list{max-height:min(42vh,360px);overflow:auto;margin:0;padding:3px 6px 6px;border-top:1px solid var(--line-soft);list-style:none}
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
.review-directory-comments{display:flex;align-items:center;gap:2px;min-width:24px;color:var(--faint);font:10px var(--mono)}
.review-directory-comments .i{width:12px;height:12px}
.review-directory-item .review-controls{opacity:1}
.review-directory-item .review-comment{display:none}
.review-directory-item .review-decision-note{display:none}
.review-directory-empty{margin:0;padding:8px 10px;border-top:1px solid var(--line-soft);color:var(--faint);font:11px var(--ui)}

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
.review-decision-note{max-width:220px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;padding-right:4px;color:var(--muted);font:11px var(--ui)}
.review-decision-note[hidden]{display:none}
.review-decision-group{display:inline-flex;align-items:center;gap:1px;padding:2px;border:1px solid var(--line-soft);border-radius:7px;background:var(--bg-subtle)}
.review-decision{position:relative;display:grid;place-items:center;width:25px;height:23px;padding:0;border:0;border-radius:5px}
.review-decision .i{width:15px;height:15px}
.review-decision.approve{background:#e6ffec80;color:#2da44e}
.review-decision.reject{background:#ffebe980;color:#cf222e}
.review-decision.approve:hover{background:#dafbe1}
.review-decision.reject:hover{background:#ffcecb}
.review-decision[aria-pressed=true].approve{background:#dafbe1;color:var(--green)}
.review-decision[aria-pressed=true].reject{background:#ffcecb;color:var(--red)}
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
.review-controls.decision-changed .review-decision[aria-pressed=true]{animation:review-pop .5s cubic-bezier(.2,.85,.25,1.35)}
.review-decision-compose{position:absolute;z-index:28;top:calc(100% + 6px);right:0;width:min(340px,80vw);padding:8px;border:1px solid var(--line);border-radius:8px;background:var(--bg);box-shadow:var(--shadow);opacity:0;transform:translateY(-4px);pointer-events:none;transition:opacity .14s ease,transform .14s ease}
.review-decision-compose[hidden]{display:none}
.review-decision-compose.open{opacity:1;transform:none;pointer-events:auto}
.review-decision-compose textarea{display:block;width:100%;min-height:58px;padding:6px 8px;border:1px solid var(--line);border-radius:var(--radius);resize:vertical;font:12.5px var(--ui)}
.review-decision-compose>div{display:flex;align-items:center;justify-content:flex-end;gap:4px;margin-top:6px}
.review-decision-compose .btn-primary{padding:4px 10px}
@keyframes review-pop{0%{transform:scale(.75)}55%{transform:scale(1.18)}100%{transform:scale(1)}}
.fragment{position:relative;scroll-margin-top:calc(var(--top) + 12px);margin:14px 0;padding:0 0 0 12px;border-left:2px solid transparent;outline:0}
.fragment.active-fragment{border-left-color:#c8e1ff}
.fragment-head{display:flex;justify-content:flex-end;gap:8px;align-items:center;min-height:22px}
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
.fragment-markdown .footnote-ref:hover,.fragment-markdown .footnote-ref:focus-visible{background:#dbeafe;color:var(--accent)}
.fragment-markdown .footnote-ref.diff-citation{background:#e8f2ff;color:var(--accent);cursor:pointer}
.fragment-markdown .footnotes{margin-top:24px;color:var(--muted);font-size:12.5px}
.fragment-markdown .footnotes hr{height:1px;margin:0 0 10px;border:0;background:var(--line-soft)}
.fragment-markdown .footnotes ol{margin:0;padding-left:24px}
.fragment-markdown .footnotes li{padding:2px 0 2px 4px}
.fragment-markdown .footnotes p{margin:.35em 0}
.fragment-markdown .footnote-backref{color:var(--muted);text-decoration:none}
.fragment-markdown .footnotes .content-landmark-text{background:#f5f9ff}

/* Quiet controls --------------------------------------------------------- */
.btn,button{font:12px var(--ui);border:1px solid transparent;border-radius:var(--radius);padding:4px 9px;background:var(--bg-inset);color:var(--ink)}
.btn:hover,button:hover{background:#e2e6ea}
.btn-primary{background:var(--accent);border-color:var(--accent);color:#fff}
.btn-primary:hover{background:#0860c4}
.btn-subtle{background:transparent;color:var(--muted)}
.icon-button{display:inline-grid;place-items:center;width:24px;height:24px;padding:0;border:0;background:transparent;color:var(--muted)}
.icon-button:hover{background:var(--bg-inset);color:var(--ink)}
.icon-button[aria-pressed=true]{background:var(--accent-soft);color:var(--accent)}
.diff-button{display:inline-flex;align-items:center;gap:4px;width:auto;padding:0 6px;font:11px var(--mono)}
.permalink{position:relative;display:inline-grid;place-items:center;width:24px;height:24px;padding:0;border:0;background:transparent;color:var(--faint)}
.permalink:hover{background:var(--bg-inset);color:var(--accent)}
.permalink.copied:after{position:absolute;right:0;bottom:calc(100% + 4px);z-index:5;content:'Copied';padding:2px 6px;border-radius:4px;background:var(--ink);color:#fff;font:11px var(--ui);white-space:nowrap}
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
.landmark-affordance{display:inline-flex;align-items:center;gap:1px;padding:1px;border-radius:var(--radius);background:#fffffff2;border:1px solid var(--line);box-shadow:var(--shadow);opacity:0;transition:opacity .12s}
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
.suggestion-code{display:block;margin:7px 0;padding:8px 10px;border-radius:var(--radius);background:var(--add-bg);color:#0a3622;white-space:pre-wrap;font:12px var(--mono)}
.message{scroll-margin-top:calc(var(--top) + 12px);margin:7px 0;padding-top:6px;border-top:1px solid var(--line-soft)}
.message-fragment{font-size:12.5px}
.message-fragment iframe{min-height:220px}
.reply{display:grid;grid-template-columns:1fr auto;gap:5px;margin-top:7px}
.reply input:not([type=hidden]),.dialog-form input,.dialog-form textarea,.diff-compose input,.diff-compose textarea{width:100%;padding:5px 8px;border:1px solid var(--line);border-radius:var(--radius);background:var(--bg)}
.reply input[type=file],.dialog-form input[type=file],.diff-compose input[type=file]{border:0;padding:0;font-size:11px;color:var(--muted)}
.thread-state{margin-top:5px}
.text-anchor{display:block;margin:5px 0;padding:2px 6px;border-radius:3px;background:#fff3c4;font-size:12px}

/* Annotation toolbox ----------------------------------------------------- */
.annotation-toolbox{position:fixed;z-index:50;left:calc(50% + 132px);transform:translateX(-50%);bottom:16px;display:flex;align-items:center;gap:1px;padding:4px;background:var(--bg);color:var(--ink);border:1px solid var(--line);border-radius:9px;box-shadow:var(--shadow)}
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
.dialog-form{padding:12px}
.dialog-form textarea{min-height:70px;margin-bottom:9px;font:13px var(--ui)}
.annotation-compose{position:fixed;z-index:90;right:18px;bottom:64px;width:min(440px,calc(100vw - 36px));display:none;box-shadow:var(--shadow)}
.annotation-compose.open{display:block}
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
.drawer-body .file-head{top:0}
.drawer-body .diff-column-head{top:38px}

/* Linked code (drawer contents) ------------------------------------------ */
.attached-code-summary{display:flex;align-items:center;gap:10px;padding:7px 12px;border-bottom:1px solid var(--line-soft);background:var(--bg-subtle);color:var(--muted);font:11px var(--mono)}
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
.attached-file-diff{border-top:1px solid var(--line-soft);overflow:auto}
.attached-file-actions{display:flex;align-items:center;gap:8px;padding:5px 10px;background:var(--bg-subtle);color:var(--faint);font:11px var(--mono);border-bottom:1px solid var(--line-soft)}
.attached-file-actions a{display:inline-flex;align-items:center;gap:4px;margin-left:auto;color:var(--accent);text-decoration:none}
.attached-columns{position:static!important;top:auto!important}
.attached-file-diff .diff-row.linked-evidence{box-shadow:inset 3px 0 var(--accent)}
.attached-file-diff .diff-row.linked-evidence .line-no:first-child{color:var(--accent)}
.attached-file-diff.loading [data-linked-diff-rows]{opacity:.55}
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
.related-chapter-link:hover{color:var(--accent)}
.related-fragment{display:block;margin-top:6px;padding:6px 8px;border-radius:var(--radius);background:var(--bg-subtle);color:var(--ink);text-decoration:none}
.related-fragment:hover{background:var(--sel)}
.related-fragment strong{display:block;font-size:12.5px}
.related-fragment span{display:block;margin-top:2px;color:var(--muted);font-size:11.5px;line-height:1.45}
.related-saga>p{color:var(--muted);font-size:12px}

/* Diff surface ----------------------------------------------------------- */
.file-diff{scroll-margin-top:calc(var(--top) + 36px);margin:0;background:var(--code-bg)}
.file-head{position:sticky;top:calc(var(--top) + 35px);z-index:5;display:flex;align-items:center;gap:8px;padding:6px clamp(8px,1.4vw,14px);background:var(--bg-subtle);border-bottom:1px solid var(--line)}
.file-head code{font:600 12.5px var(--mono);overflow-wrap:anywhere}
.file-head .counts{margin-left:auto;font:11px var(--mono);color:var(--faint)}
.file-head .counts .add{color:var(--green)}
.file-head .counts .del{color:var(--red)}
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
.diff-row.context{color:var(--ink)}
.diff-row.new{background:var(--add-bg);box-shadow:inset 2px 0 var(--add-line)}
.diff-row.old{background:var(--del-bg);box-shadow:inset 2px 0 var(--del-line)}
.diff-row.event{background:var(--bg-subtle);color:var(--muted)}
.diff-row.selected{outline:2px solid var(--accent);outline-offset:-2px;z-index:1}
.line-no{display:block;padding:1px 8px;text-align:right;color:var(--faint);user-select:none;background:var(--code-gutter);border-right:1px solid var(--line-soft)}
.diff-row.new .line-no{background:#d9f6e0}
.diff-row.old .line-no{background:#ffdcd8}
.line-select{width:100%;height:100%;min-height:20px;padding:1px 8px;border:0;border-radius:0;background:var(--code-gutter);color:var(--faint);text-align:right;font:inherit;border-right:1px solid var(--line-soft)}
.diff-row.new .line-select{background:#d9f6e0}
.diff-row.old .line-select{background:#ffdcd8}
.line-select:hover,.line-select:focus-visible{background:var(--accent);color:#fff}
.sign{padding:1px 4px;color:var(--muted);text-align:center;user-select:none}
.code-line{padding:1px 10px;white-space:pre;overflow:visible;tab-size:4}
.line-actions{display:flex;justify-content:flex-end;gap:1px;padding:0 4px;opacity:0}
.diff-row:hover .line-actions,.diff-row:focus-within .line-actions{opacity:1}
.line-actions .icon-button{width:20px;height:20px;background:var(--bg);border:1px solid var(--line)}
.diff-thread-wrap{grid-column:4/6;padding:0 10px 6px}
.context-expander{display:flex;align-items:center;justify-content:center;gap:6px;width:100%;padding:2px;border:0;border-radius:0;background:var(--bg-inset);color:var(--accent);border-block:1px solid var(--line-soft);font:11px var(--mono)}
.context-expander:hover{background:var(--accent-soft)}
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
.tok-keyword{color:#cf222e}
.tok-string{color:#0a3069}
.tok-number{color:#0550ae}
.tok-comment{color:#6e7781}
.tok-type{color:#953800}
.tok-property{color:#8250df}
.tok-punctuation{color:#57606a}
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
.manifest-tools{position:sticky;top:0;z-index:4;display:flex;gap:8px;align-items:center;padding:8px 0;background:#ffffffee;backdrop-filter:blur(6px);border-bottom:1px solid var(--line-soft)}
.manifest-modes{display:flex;gap:2px;padding:2px;border-radius:var(--radius);background:var(--bg-inset)}
.manifest-modes button{background:transparent;color:var(--muted);padding:3px 9px;font-size:12px}
.manifest-modes button[aria-pressed=true]{background:var(--bg);color:var(--ink);box-shadow:0 1px 2px #1f232826}
.manifest-tools .manifest-metric{color:var(--faint);font:11px var(--mono)}
.manifest-tools .tree-search{max-width:280px;margin-left:auto;flex:1}
.manifest-tools input{min-width:0;width:100%;height:26px;padding:0 8px 0 25px;border:1px solid var(--line);border-radius:var(--radius);background:var(--bg);font-size:12px}
.manifest-alert{display:flex;gap:9px;align-items:flex-start;margin:12px 0;padding:10px 12px;border:1px solid #e5b3ae;border-left:3px solid var(--red);border-radius:var(--radius);background:#fff5f4;font-size:12.5px}
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
.manifest-file.has-gap>summary{background:#fff5f4}
.manifest-file.has-gap>summary .mfile-name{color:var(--red)}
.manifest-file-stats{margin-left:auto;padding-left:10px;color:var(--faint);font:11px var(--mono);white-space:nowrap}
.manifest-file-stats .add{color:var(--green)}
.manifest-file-stats .del{color:var(--red)}
.manifest-file-stats .gap{color:var(--red);font-weight:600}
.manifest-file-detail{margin-left:calc(20px + var(--depth,0) * 13px);border-left:1px solid var(--line-soft)}
.manifest-file-diff{max-height:min(54vh,620px);overflow:auto;border-block:1px solid var(--line-soft)}
.manifest-file-diff .diff-lines{min-width:580px}
.manifest-file-diff .diff-row{grid-template-columns:46px 46px 18px minmax(240px,1fr) 0}
.manifest-map-heading{padding:5px 10px;border-bottom:1px solid var(--line-soft);background:var(--bg-subtle);color:var(--faint);font:600 10px var(--ui);letter-spacing:.045em;text-transform:uppercase}
.manifest-file-detail>.manifest-rows{padding-left:0}
.manifest-rows{padding-left:calc(20px + var(--depth,0) * 13px);border-left:0}
.manifest-row{display:grid;grid-template-columns:minmax(220px,.85fr) minmax(280px,1.15fr);border-top:1px solid var(--line-soft)}
.manifest-row.unmapped{background:#fff5f4}
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
.manifest-owner-missing,.manifest-gap{color:var(--red);font-size:11.5px}
.manifest-target-title{display:flex;min-width:0;align-items:baseline;gap:7px}
.manifest-target-title strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font:500 12.5px var(--ui)}
.manifest-target-title small{color:var(--faint);font:11px var(--mono)}
.manifest-target{border-bottom:1px solid var(--line-soft)}
.manifest-target>summary{color:var(--ink)}
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
.manifest-orphan{display:flex;align-items:center;gap:8px;margin-top:6px;padding:8px 10px;border:1px solid #e5b3ae;border-radius:var(--radius);background:#fff5f4;font-size:12.5px}
.manifest-orphan>.i{color:var(--red);flex:none}
.manifest-orphan strong,.manifest-orphan small{display:block}
.manifest-orphan small{color:var(--muted);font-size:11.5px}
.manifest-orphan a{margin-left:auto;color:var(--accent);text-decoration:none;white-space:nowrap}

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
.annotation-toolbox{left:8px;right:8px;transform:none;overflow-x:auto;justify-content:flex-start}
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
.diff-drawer{width:100vw}
}
`
