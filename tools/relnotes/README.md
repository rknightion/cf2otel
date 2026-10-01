# Release notes coverage input

`relnotes` accepts **official GitHub rendered release-note text**, the REST API
`body_text` field returned with its text media type. Save that field as a text
file, then run the existing `just relnotes-check` recipe or the CLI:

```sh
go run ./tools/relnotes --notes rendered-notes.txt --notes-format rendered
```

`--notes-format` defaults to `rendered`, so existing recipe calls remain valid.
The checker does not fetch, render or parse raw Markdown. Do not supply the API's
raw `body` field, a Markdown source file, or HTML (`body_html`). Explicit
`--notes-format markdown` or `--notes-format raw` returns an unsupported-input
error (exit 2). Unknown formats also return exit 2.

As a guard against accidentally passing raw input under the default format,
Markdown inline/reference link metadata, reference definitions, HTML tags and
HTML comments are rejected with an explicit raw-Markdown error. This is an
input guard, not a Markdown renderer or a complete format detector. Ambiguous
literal text resembling those constructs is rejected too, including labels that
span lines (also with backslash escapes). This conservative policy applies to
inline links, reference links and reference definitions, even when a Markdown
renderer would treat a particular multiline construct as literal text. A
plain-text file cannot prove its provenance; callers must obtain the official
rendered field.

Coverage requires a visible release description matching each required commit's
subject description. Case and whitespace normalization and whole-phrase
boundaries still apply. A rendered issue reference such as `(#42)` may follow a
visible description, but a reference alone cannot replace that description.
Hidden Markdown link destinations or titles are never accepted as evidence.

One entry covers at most one commit with the same normalized description.
Existing patch-identical released-commit exclusions and exact-subject empty
restore aliases are unchanged; issue references do not add aliases or waive
coverage. Missing coverage returns exit 1; complete coverage returns exit 0.
