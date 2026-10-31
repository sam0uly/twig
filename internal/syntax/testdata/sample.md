# twig

A diff viewer that stays current while you edit.

## Why it is fast

The engine anchors on unique blocks first and only runs Myers inside the gaps,
so a rewrite that would pin a core for a minute is a few milliseconds.

- files come as a flat list or a tree
- `s` stages, `u` unstages

```go
func main() {
	println("a // not a comment")
}
```

> A quote from the docs.
