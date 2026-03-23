# Code exploration guidelines

Choose tools based on intent. Minimize full-file reads.

## Exact lookup
Use Grep or Glob for:
- exact identifiers, strings, constants
- file paths and names
- known symbols and imports

Do not use `cs search` for exact matches.

## Conceptual discovery
Use `cs search "<question>" --path .` when:
- you need to understand behavior or architecture
- relevant files are not known yet

Use it to identify a small set of candidate files before reading anything.

## Targeted reading
Use `cs extract -f <file> -s <symbol>` for focused inspection.

Do not read full files by default. Instead:
- extract the specific symbol you need
- read full files only when surrounding context is required

Do not re-read files you have already seen in this conversation.

## Cross-file navigation
Use instead of manual grep chains:
- `cs refs <symbol>` for references
- `cs callers <symbol>` for call hierarchy
- `cs implements <symbol>` for implementations

## Exploration pattern
For non-trivial tasks:
1. narrow scope with `cs search`
2. inspect symbols with `cs extract`
3. expand to full-file reads only where necessary

Do not read multiple large files to build understanding.
Do not read a file "just to be sure" if you already have the information you need.

## Routing summary
- known target -> Grep / Glob
- unknown area -> `cs search`
- single symbol -> `cs extract`
- relationships -> `cs refs` / `cs callers` / `cs implements`
