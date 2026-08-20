package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppJavaScriptSyntaxAndRangeSelectionContract(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not installed")
	}
	source := strings.Replace(appJavaScript, "})();", "globalThis.reviewSagaTest = {languageForPath, selectedRangeURI, normalizedAnnotationColor, colorWithAlpha, shortcutDirection, translateShape, stepShapeDraftHistory};})();", 1)
	prelude := `globalThis.document={querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{}};
globalThis.location={href:'http://127.0.0.1/?view=code',pathname:'/',search:'?view=code',hash:''};
globalThis.history={pushState:()=>{}};globalThis.addEventListener=()=>{};globalThis.innerWidth=1400;
`
	checks := `
if(reviewSagaTest.languageForPath('src/main.go')!=='go')throw new Error('Go language detection failed');
if(reviewSagaTest.languageForPath('web/view.tsx')!=='javascript')throw new Error('TSX language detection failed');
const rows=[{dataset:{line:'11',diffRef:'saga-diff://v1/line?base=a&end=11&head=b&path=app.go&repository=https%3A%2F%2Fe.test%2Fa.git&side=new&start=11'}},{dataset:{line:'13'}}];
const range=reviewSagaTest.selectedRangeURI(rows);if(!range.includes('start=11')||!range.includes('end=13')||!range.includes('side=new'))throw new Error('qualified range selection failed');
if(reviewSagaTest.normalizedAnnotationColor('#A1b2C3')!=='#a1b2c3')throw new Error('annotation color normalization failed');
if(reviewSagaTest.normalizedAnnotationColor('red')!=='#d04832')throw new Error('unsafe annotation color fallback failed');
if(reviewSagaTest.colorWithAlpha('#112233')!=='#11223355')throw new Error('highlight alpha failed');
if(reviewSagaTest.shortcutDirection({key:'z',metaKey:true})!=='undo')throw new Error('Command+Z undo failed');
if(reviewSagaTest.shortcutDirection({key:'Z',ctrlKey:true,shiftKey:true})!=='redo')throw new Error('Ctrl+Shift+Z redo failed');
if(reviewSagaTest.shortcutDirection({key:'y',ctrlKey:true})!=='redo')throw new Error('Ctrl+Y redo failed');
if(reviewSagaTest.shortcutDirection({key:'z',ctrlKey:true,altKey:true})!=='')throw new Error('modified shortcut should be ignored');
const movedRect=reviewSagaTest.translateShape({type:'rect',x:.8,y:.8,width:.2,height:.2},.4,.4);
if(movedRect.x!==.8||movedRect.y!==.8)throw new Error('rectangle movement must remain normalized');
const movedPath=reviewSagaTest.translateShape({type:'path',points:[{x:.1,y:.2},{x:.3,y:.4}]},.2,-.1);
if(Math.abs(movedPath.points[0].x-.3)>.0001||Math.abs(movedPath.points[0].y-.1)>.0001)throw new Error('freehand movement failed');
const empty={type:'region',coordinate_space:'normalized',shapes:[]};
const rectangle={type:'region',coordinate_space:'normalized',shapes:[{type:'rect',x:.1,y:.1,width:.2,height:.2}]};
const freehand={type:'drawing',coordinate_space:'normalized',shapes:[...rectangle.shapes,{type:'path',points:[{x:.2,y:.2},{x:.4,y:.4}]}]};
const draft={anchor:freehand,undo:[empty,rectangle],redo:[]};
if(!reviewSagaTest.stepShapeDraftHistory(draft,'undo')||draft.anchor.shapes.length!==1||draft.redo.length!==1)throw new Error('gesture undo failed');
if(!reviewSagaTest.stepShapeDraftHistory(draft,'redo')||draft.anchor.shapes.length!==2||draft.undo.length!==2)throw new Error('gesture redo failed');
`
	path := filepath.Join(t.TempDir(), "appjs-check.js")
	if err := os.WriteFile(path, []byte(prelude+source+checks), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
		t.Fatalf("JavaScript syntax check failed: %v\n%s", err, output)
	}
	if output, err := exec.Command(node, path).CombinedOutput(); err != nil {
		t.Fatalf("JavaScript interaction contract failed: %v\n%s", err, output)
	}
}
