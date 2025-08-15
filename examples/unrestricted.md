# Unrestricted toolbelt examples

Warning: These prompts exercise powerful tools (`exec`, file system). Enable only in a sandboxed environment. See README “Unrestricted tools warning”.

## Prompt 1: Write, build, and run a tiny Go program
Paste the following as your `-prompt` when running `agentcli` with the unrestricted tools enabled in `tools.json`.

"""
Create a new Go module under `play/hello` and a `main.go` that prints `hello from toolbelt`. Use these steps deterministically:
- Use `fs_mkdirp` to create `play/hello`.
- Use `fs_write_file` to write `play/hello/main.go` with a minimal Go program.
- Use `exec` to run `go mod init example.com/hello` in `play/hello`.
- Use `exec` to run `go build -o ../../bin/hello` from `play/hello`.
- Use `exec` to run `../../bin/hello` and capture stdout.
Return the final program stdout only.
"""

Suggested `main.go` content:
```go
package main
import "fmt"
func main(){ fmt.Println("hello from toolbelt") }
```

## Prompt 2: Edit a file and verify contents
"""
Create `scratch/note.txt` with the line `alpha`. Append a second line `beta`. Then read the file back and return its full contents. Steps:
- `fs_write_file` to create `scratch/note.txt` with base64("alpha\n")
- `fs_append_file` to append base64("beta\n")
- `fs_read_file` to read back and decode the file
Return the decoded text only.
"""

## Prompt 3: Move, overwrite, and delete
"""
Create `scratch/a.txt` with `A`. Move it to `scratch/b.txt`. Overwrite `scratch/b.txt` with `B` using `fs_move` and `overwrite:true`. Then remove the `scratch` directory recursively with `fs_rm` and confirm it is gone.
"""
