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
	source := strings.Replace(appJavaScript, "})();", "globalThis.reviewSagaTest = {languageForPath, selectedRangeURI};})();", 1)
	prelude := `globalThis.document={querySelector:()=>null,querySelectorAll:()=>[],addEventListener:()=>{}};
globalThis.location={href:'http://127.0.0.1/?view=code',pathname:'/',search:'?view=code',hash:''};
globalThis.history={pushState:()=>{}};globalThis.addEventListener=()=>{};globalThis.innerWidth=1400;
`
	checks := `
if(reviewSagaTest.languageForPath('src/main.go')!=='go')throw new Error('Go language detection failed');
if(reviewSagaTest.languageForPath('web/view.tsx')!=='javascript')throw new Error('TSX language detection failed');
const rows=[{dataset:{line:'11',diffRef:'saga-diff://v1/line?base=a&end=11&head=b&path=app.go&repository=https%3A%2F%2Fe.test%2Fa.git&side=new&start=11'}},{dataset:{line:'13'}}];
const range=reviewSagaTest.selectedRangeURI(rows);if(!range.includes('start=11')||!range.includes('end=13')||!range.includes('side=new'))throw new Error('qualified range selection failed');
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
