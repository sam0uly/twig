package syntax

import (
	"path/filepath"
	"strings"
)

// The tables below are the whole language database: comments, string
// delimiters and reserved words. There are no grammar files, no regular
// expressions and no embedded data, so a language costs a few hundred bytes of
// read-only strings and adding one is a table entry plus a fixture.
//
// Each entry is a guess about what a reader of a diff needs, not a
// specification. Where a rule would cost more than it buys, the table leaves it
// out and says so: rust has no character literal because `'a` is far more often
// a lifetime, and markdown has no table at all because its structure is line
// based rather than token based.

// goLang is Go.
var goLang = &Language{
	Name:    "go",
	Bools:   "true false",
	Exts:    []string{".go"},
	LineCmt: []string{"//"},
	Block:   [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
		{Quote: '`', MultiLine: true},
	},
	Keywords: "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var",
	Types:    "any bool byte comparable complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr",
	Consts:   "nil true false iota",
	Hints:    true,
}

// rustLang is Rust.
var rustLang = &Language{
	Name:     "rust",
	Bools:    "true false",
	DocCmt:   []string{"///", "//!"},
	DocBlock: true,
	Nest:     true,
	Exts:     []string{".rs"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Raw:      []RawKind{RawRust},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	Keywords: "as async await break const continue crate dyn else enum extern fn for if impl in let loop match mod move mut pub ref return static struct super trait type unsafe use where while",
	Types:    "bool char f32 f64 i8 i16 i32 i64 i128 isize str u8 u16 u32 u64 u128 usize String Vec Option Result Box Self",
	Consts:   "true false None Some Ok Err",
	Hints:    true,
}

// pythonLang is Python.
var pythonLang = &Language{
	Name:    "python",
	Bools:   "True False",
	Exts:    []string{".py", ".pyi", ".pyw"},
	LineCmt: []string{"#"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\', Repeat: true},
		{Quote: '\'', Escape: '\\', Repeat: true},
	},
	Keywords: "and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield match case",
	Types:    "int float complex str bytes bool list dict set tuple frozenset range object type self cls",
	Consts:   "True False None NotImplemented Ellipsis",
	Hints:    true,
}

// jsKeywords is shared by javascript and typescript.
const jsKeywords = "async await break case catch class const continue debugger default delete do else export extends finally for from function get if import in instanceof let new of return set static super switch this throw try typeof var void while with yield"

// javascriptLang is JavaScript.
var javascriptLang = &Language{
	Name:     "javascript",
	JSX:      true,
	Regex:    true,
	Bools:    "true false",
	DocBlock: true,
	KeySep:   ':',
	Exts:     []string{".js", ".jsx", ".mjs", ".cjs"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
		{Quote: '`', Escape: '\\', MultiLine: true},
	},
	Keywords: jsKeywords,
	Consts:   "true false null undefined NaN Infinity",
	Hints:    true,
}

// typescriptLang is TypeScript, which is JavaScript plus its own words.
var typescriptLang = &Language{
	Name:     "typescript",
	Regex:    true,
	Bools:    "true false",
	DocBlock: true,
	KeySep:   ':',
	Exts:     []string{".ts", ".mts", ".cts"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
		{Quote: '`', Escape: '\\', MultiLine: true},
	},
	Keywords: jsKeywords + " abstract as assert declare enum implements infer interface is keyof namespace private protected public readonly satisfies type",
	Types:    "any bigint boolean never number object string symbol unknown void Array Promise Record Partial",
	Consts:   "true false null undefined NaN Infinity",
	Hints:    true,
}

// typescriptJSX is TypeScript with JSX. It is a separate table because JSX
// changes what "<" means, and in plain TypeScript "<Foo>x" is a type assertion.
var typescriptJSX = &Language{
	Name:     "tsx",
	Bools:    "true false",
	JSX:      true,
	Regex:    true,
	DocBlock: true,
	KeySep:   ':',
	Exts:     []string{".tsx"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
		{Quote: '`', Escape: '\\', MultiLine: true},
	},
	Keywords: jsKeywords + " abstract as assert declare enum implements infer interface is keyof namespace private protected public readonly satisfies type",
	Types:    "any bigint boolean never number object string symbol unknown void Array Promise Record Partial",
	Consts:   "true false null undefined NaN Infinity",
	Hints:    true,
}

// cKeywords and cTypes are shared by C and Objective-C.
const (
	cKeywords = "auto break case const continue default do else enum extern for goto if inline register restrict return sizeof static struct switch typedef union volatile while"
	cTypes    = "char double float int long short signed unsigned void size_t ssize_t bool int8_t int16_t int32_t int64_t uint8_t uint16_t uint32_t uint64_t ptrdiff_t"
)

// cLang is C.
var cLang = &Language{
	Name:     "c",
	DocBlock: true,
	Preproc:  true,
	Exts:     []string{".c", ".h"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: cKeywords,
	Types:    cTypes,
	Consts:   "NULL true false",
}

// cppKeywords and cppTypes are shared by C++ and CUDA.
const (
	cppKeywords = "alignas alignof auto break case catch class const constexpr consteval constinit continue decltype default delete do dynamic_cast else enum explicit export extern false for friend goto if inline mutable namespace new noexcept nullptr operator override private protected public register reinterpret_cast requires return sizeof static static_assert static_cast struct switch template this throw true try typedef typeid typename union using virtual volatile while"
	cppTypes    = "bool char double float int long short signed unsigned void size_t string vector map set unordered_map unique_ptr shared_ptr optional span array tuple pair"
)

// cppLang is C++.
var cppLang = &Language{
	Name:     "cpp",
	DocBlock: true,
	Preproc:  true,
	Exts:     []string{".cc", ".cpp", ".cxx", ".hpp", ".hh", ".hxx", ".ino"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Raw:      []RawKind{RawCpp},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: cppKeywords,
	Types:    cppTypes,
	Consts:   "nullptr NULL true false",
	Hints:    true,
}

// javaLang is Java.
var javaLang = &Language{
	Name:     "java",
	Bools:    "true false",
	DocBlock: true,
	Exts:     []string{".java"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\', Repeat: true},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "abstract assert break case catch char class const continue default do else enum extends final finally float for goto if implements import instanceof int interface long native new package private protected public return sealed short static strictfp super switch synchronized this throw throws transient try var void volatile while yield record permits",
	Types:    "boolean byte double String Integer Long Double Float Boolean Object List Map Set Optional Stream",
	Consts:   "true false null",
	Hints:    true,
}

// csharpLang is C#.
var csharpLang = &Language{
	Name:     "csharp",
	Bools:    "true false",
	DocCmt:   []string{"///"},
	DocBlock: true,
	Exts:     []string{".cs"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Raw:      []RawKind{RawCSharpVerbatim, RawCSharpRaw},
	Interp:   true,
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "abstract as async await base break case catch checked class const continue default delegate do else enum event explicit extern finally fixed for foreach goto if implicit in init internal is lock namespace new operator out override params private protected public readonly record ref return sealed sizeof stackalloc static struct switch this throw try typeof uint ulong unchecked unsafe ushort using var virtual void volatile while",
	Types:    "bool byte char decimal double float int long object sbyte short string Int32 Int64 Double String Boolean Object List Dictionary HashSet Task",
	Consts:   "true false null",
	Hints:    true,
}

// rubyLang is Ruby.
var rubyLang = &Language{
	Name:    "ruby",
	Atoms:   true,
	Regex:   true,
	Heredoc: hdRuby,
	Bools:   "true false",
	Exts:    []string{".rb", ".rake", ".gemspec"},
	Files:   []string{"Gemfile", "Rakefile"},
	LineCmt: []string{"#"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "alias and begin break case class def defined do else elsif end ensure for if in module next not or redo rescue retry return self super then undef unless until when while yield require require_relative attr_accessor attr_reader attr_writer include extend",
	Types:    "String Integer Float Array Hash Symbol Proc Range Struct",
	Consts:   "true false nil",
	Hints:    true,
}

// phpLang is PHP, the code alone. A .php page embeds it; see phpPageLang.
var phpLang = &Language{
	Name:     "php",
	Bools:    "true false TRUE FALSE",
	Heredoc:  hdPHP,
	DocBlock: true,
	Vars:     true,
	Attrs:    true,
	LineCmt:  []string{"//", "#"},
	Block:    [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "abstract and array as break callable case catch class clone const continue declare default do echo else elseif empty enum extends final finally fn for foreach function global goto if implements include include_once instanceof insteadof interface isset list match namespace new or print private protected public readonly require require_once return static switch throw trait try unset use var while xor yield",
	Types:    "int float string bool array object mixed void never self parent",
	Consts:   "true false null TRUE FALSE NULL",
	Hints:    true,
}

// phpPageLang is a .php page: HTML that scaffolds PHP islands inside
// <?php ... ?>. It reuses the shapes of htmlLang here rather than embedding
// htmlLang as a region, because a page starts in HTML before the first tag.
var phpPageLang = &Language{
	Name: "php",
	Embed: []Embed{
		{Open: "<?php", Close: "?>", Lang: phpLang},
	},
	Markup: true,
	Hyphen: true,
	Exts:   []string{".php"},
	Block:  [2]string{"<!--", "-->"},
	Strings: []StringRule{
		{Quote: '"'},
		{Quote: '\''},
	},
}

// shellLang is POSIX shell and its common dialects.
var shellLang = &Language{
	Name:    "shell",
	Bools:   "true false",
	Heredoc: hdShell,
	Vars:    true,
	Exts:    []string{".sh", ".bash", ".zsh", ".fish", ".ksh"},
	Files:   []string{"Makefile", "makefile", "GNUmakefile", "Dockerfile", "Containerfile", "CMakeLists.txt", ".gitignore", ".gitattributes", ".dockerignore", ".env", ".bashrc", ".zshrc", ".profile"},
	LineCmt: []string{"#"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "if then else elif fi for while until do done case esac in function select local export readonly declare return break continue shift set unset trap source eval exec exit",
	Consts:   "true false",
}

// luaLang is Lua.
var luaLang = &Language{
	Name:    "lua",
	Exts:    []string{".lua"},
	LineCmt: []string{"--"},
	Block:   [2]string{"--[[", "]]"},
	Raw:     []RawKind{RawLua},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "and break do else elseif end false for function goto if in local nil not or repeat return then true until while",
}

// sqlLang is SQL.
var sqlLang = &Language{
	Name:    "sql",
	Exts:    []string{".sql"},
	LineCmt: []string{"--"},
	Block:   [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '\'', Escape: '\\'},
		{Quote: '"'},
	},
	Keywords: "add all alter and any as asc begin between by case cast column commit constraint create cross current_date current_time current_timestamp database default delete desc distinct drop else end escape exists false foreign from full grant group having if in index inner insert intersect into is join key left like limit not null offset on or order outer primary references right rollback select set table then true union unique update using values view when where with",
	// SQL spells its reserved words in any case, and the table is lowercase.
	NoCase: true,
}

// jsonLang is JSON and its comment-tolerant dialects.
var jsonLang = &Language{
	Name:    "json",
	Bools:   "true false",
	KeySep:  ':',
	Exts:    []string{".json", ".jsonc", ".jsonl", ".ndjson", ".webmanifest"},
	LineCmt: []string{"//"},
	Block:   [2]string{"/*", "*/"},
	Strings: []StringRule{{Quote: '"', Escape: '\\'}},
	Consts:  "true false null",
}

// yamlLang is YAML.
var yamlLang = &Language{
	Name:    "yaml",
	Bools:   "true false",
	Hyphen:  true,
	KeySep:  ':',
	Exts:    []string{".yml", ".yaml"},
	LineCmt: []string{"#"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\''},
	},
	Consts: "true false null yes no on off",
}

// tomlLang is TOML.
var tomlLang = &Language{
	Name:    "toml",
	Bools:   "true false",
	Hyphen:  true,
	KeySep:  '=',
	Exts:    []string{".toml"},
	Files:   []string{"Cargo.lock"},
	LineCmt: []string{"#"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\''},
	},
	Consts: "true false",
}

// htmlLang is HTML. A name after "<" is a tag and a name inside a tag is an
// attribute, both straight from position, so the table carries no element or
// attribute word list: one would only leak into the prose between tags.
var htmlLang = &Language{
	Name: "html",
	// A component or page carries other languages: the script block is
	// JavaScript or TypeScript, the style block is CSS or a preprocessor.
	Embed: []Embed{
		{
			Open: "<script", Close: "</script", Lang: javascriptLang, Attr: "lang",
			ByAttr: map[string]*Language{
				"ts": typescriptLang, "tsx": typescriptLang, "mts": typescriptLang,
				"cts": typescriptLang, "jsx": javascriptLang, "js": javascriptLang,
			},
		},
		{
			Open: "<style", Close: "</style", Lang: cssLang, Attr: "lang",
			ByAttr: map[string]*Language{
				"scss": cssLang, "sass": cssLang, "less": cssLang, "styl": cssLang,
			},
		},
	},
	Hyphen:   true,
	Markup:   true,
	Mustache: true,
	Exts:     []string{".html", ".htm", ".xhtml"},
	Block:    [2]string{"<!--", "-->"},
	Strings: []StringRule{
		{Quote: '"'},
		{Quote: '\''},
	},
}

// vueLang is a Vue or Svelte component: HTML whose template carries JavaScript
// expressions in mustaches and in the values of :attr, @attr, #attr and v-
// directives.
var vueLang = &Language{
	Name: "vue",
	// A component carries other languages too: the script block is JavaScript
	// or TypeScript, the style block is CSS or a preprocessor.
	Embed: []Embed{
		{
			Open: "<script", Close: "</script", Lang: javascriptLang, Attr: "lang",
			ByAttr: map[string]*Language{
				"ts": typescriptLang, "tsx": typescriptLang, "mts": typescriptLang,
				"cts": typescriptLang, "jsx": javascriptLang, "js": javascriptLang,
			},
		},
		{
			Open: "<style", Close: "</style", Lang: cssLang, Attr: "lang",
			ByAttr: map[string]*Language{
				"scss": cssLang, "sass": cssLang, "less": cssLang, "styl": cssLang,
			},
		},
	},
	Hyphen:   true,
	Markup:   true,
	Mustache: true,
	Vue:      true,
	Exts:     []string{".vue", ".svelte"},
	Block:    [2]string{"<!--", "-->"},
	Strings: []StringRule{
		{Quote: '"'},
		{Quote: '\''},
	},
}

// cssLang is CSS and its preprocessors.
var cssLang = &Language{
	Name:     "css",
	Hyphen:   true,
	DocBlock: true,
	KeySep:   ':',
	Exts:     []string{".css", ".scss", ".sass", ".less"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "charset font-face import keyframes layer media mixin namespace page supports",
}

// kotlinLang is Kotlin.
var kotlinLang = &Language{
	Name:     "kotlin",
	Exts:     []string{".kt", ".kts"},
	LineCmt:  []string{"//"},
	DocBlock: true,
	Nest:     true,
	Block:    [2]string{"/*", "*/"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\', Repeat: true}, {Quote: '\'', Escape: '\\'}},
	Keywords: "as break by catch class companion const constructor continue crossinline data delegate do dynamic else enum expect external field file finally for fun get if import in infix init inline inner interface internal is lateinit noinline object open operator out override package private protected public reified return sealed set super suspend tailrec this throw try typealias val var vararg when where while",
	Types:    "Any Array Boolean Byte Char Double Float Int List Long Map Nothing Number Pair Set Short String Triple Unit LongRange",
	Consts:   "null",
	Bools:    "true false",
	Hints:    true,
}

// swiftLang is Swift.
var swiftLang = &Language{
	Name:     "swift",
	Exts:     []string{".swift"},
	LineCmt:  []string{"//"},
	DocCmt:   []string{"///"},
	DocBlock: true,
	Nest:     true,
	Block:    [2]string{"/*", "*/"},
	Raw:      []RawKind{RawSwift},
	Strings:  []StringRule{{Quote: '"', Escape: '\\', Repeat: true}},
	Keywords: "associatedtype borrowing class consuming deinit defer do else enum extension fileprivate func guard if import in init inout internal is let mutating nonmutating open operator private protocol public repeat required rethrows return static struct subscript super switch throw try typealias var where while override convenience final lazy weak unowned async await",
	Types:    "Any AnyObject Array Bool Character Data Dictionary Double Error Float Int Optional Result Set String UInt Void",
	Consts:   "nil self",
	Bools:    "true false",
	Hints:    true,
}

// scalaLang is Scala.
var scalaLang = &Language{
	Name:     "scala",
	Atoms:    true,
	DocBlock: true,
	Nest:     true,
	Exts:     []string{".scala", ".sc"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\', Repeat: true},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "abstract case catch class def do else enum export extends final finally for forSome given if implicit import lazy match new object override package private protected return sealed super then this throw trait try type using val var while with yield",
	Types:    "Any AnyRef Boolean Byte Char Double Float Int List Long Map Nothing Option Set Short String Unit Vector",
	Consts:   "null",
	Bools:    "true false",
	Hints:    true,
}

// dartLang is Dart.
var dartLang = &Language{
	Name:     "dart",
	DocCmt:   []string{"///"},
	DocBlock: true,
	Exts:     []string{".dart"},
	LineCmt:  []string{"//"},
	Nest:     true,
	Block:    [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\', Repeat: true},
		{Quote: '\'', Escape: '\\', Repeat: true},
	},
	Keywords: "abstract as assert async await break case catch class const continue covariant default deferred do dynamic else enum export extends extension external factory final finally for get hide if implements import in interface is late library mixin new on operator part required rethrow return set show static super switch sync this throw try typedef var void while with yield",
	Types:    "bool double int num Object String List Map Set Future Stream Iterable Never",
	Consts:   "null",
	Bools:    "true false",
	Hints:    true,
}

// elixirLang is Elixir.
var elixirLang = &Language{
	Name:    "elixir",
	Atoms:   true,
	Exts:    []string{".ex", ".exs", ".eex", ".heex"},
	LineCmt: []string{"#"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\', Repeat: true},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "after alias and case catch cond def defguard defimpl defmacro defmodule defp defprotocol defstruct do else end fn for if import in not or quote raise receive require rescue reraise super throw try unless unquote use when with",
	Types:    "Atom BitString Float Function Integer List Map PID Port Range Reference Tuple",
	Consts:   "nil",
	Bools:    "true false",
	Hints:    true,
}

// erlangLang is Erlang. A leading colon is the remote-call separator in
// `lists:reverse`, not an atom marker, so the table does not read `:name` as a
// literal the way ruby and elixir do.
var erlangLang = &Language{
	Name:     "erlang",
	Exts:     []string{".erl", ".hrl"},
	LineCmt:  []string{"%"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	Literals: LitBase | LitDollar,
	Keywords: "after begin case catch cond end fun if let of query receive try when and andalso band bnot bor bsl bsr bxor div not or orelse rem xor",
	Consts:   "nil",
	Bools:    "true false",
	Hints:    true,
}

// haskellLang is Haskell.
var haskellLang = &Language{
	Name:     "haskell",
	DocBlock: true,
	Nest:     true,
	Exts:     []string{".hs", ".lhs"},
	LineCmt:  []string{"--"},
	Block:    [2]string{"{-", "-}"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	Literals: LitChar,
	Keywords: "as case class data default deriving do else forall foreign hiding if import in infix infixl infixr instance let mdo module newtype qualified then type where",
	Types:    "Bool Char Double Either Float IO Int Integer List Maybe Ordering String Word",
	Consts:   "Nothing",
	Bools:    "True False",
}

// elmLang is Elm. Its comments nest like Haskell's.
var elmLang = &Language{
	Name:     "elm",
	Nest:     true,
	Exts:     []string{".elm"},
	LineCmt:  []string{"--"},
	DocBlock: true,
	Block:    [2]string{"{-", "-}"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}, {Quote: '\'', Escape: '\\'}},
	Keywords: "alias as case else exposing if import in infix infixl infixr let module of port then type where",
	Types:    "Array Bool Char Cmd Dict Float Int List Maybe Never Program Result Set String Sub Task",
	Consts:   "Nothing",
	Bools:    "True False",
	Hints:    true,
}

// ocamlLang is OCaml.
var ocamlLang = &Language{
	Name:     "ocaml",
	Nest:     true,
	Exts:     []string{".ml", ".mli"},
	Block:    [2]string{"(*", "*)"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	Keywords: "and as assert begin class constraint do done downto else end exception external for fun function functor if in include inherit initializer lazy let match method module mutable new nonrec object of open private rec sig struct then to try type val virtual when while with",
	Types:    "bool char float int int32 int64 list option string unit bytes array",
	Consts:   "None",
	Bools:    "true false",
	Hints:    true,
}

// clojureLang is Clojure.
var clojureLang = &Language{
	Name:     "clojure",
	Atoms:    true,
	Exts:     []string{".clj", ".cljs", ".cljc", ".edn"},
	LineCmt:  []string{";"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	Keywords: "case catch cond def defmacro defmethod defmulti defn defonce defprotocol defrecord defstruct deftype do doseq dotimes doseq fn for future if if-let if-not import in let letfn loop ns quote recur refer require set! throw try use var when when-let when-not while",
	Consts:   "nil",
	Bools:    "true false",
	Hints:    true,
}

// zigLang is Zig.
var zigLang = &Language{
	Name:     "zig",
	Exts:     []string{".zig"},
	LineStr:  "\\\\",
	LineCmt:  []string{"//"},
	DocCmt:   []string{"///", "//!"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}, {Quote: '\'', Escape: '\\'}},
	Keywords: "align allowzero and anyframe anytype asm async await break callconv catch comptime const continue defer else enum errdefer error export extern fn for if inline noalias noinline nosuspend opaque or orelse packed pub resume return linksection struct suspend switch test threadlocal try union unreachable usingnamespace var volatile while",
	Types:    "anyerror bool comptime_float comptime_int f16 f32 f64 f80 f128 i8 i16 i32 i64 i128 isize u8 u16 u32 u64 u128 usize void noreturn type anyopaque",
	Consts:   "null undefined",
	Bools:    "true false",
	Hints:    true,
}

// rLang is R.
var rLang = &Language{
	Name:     "r",
	Exts:     []string{".r"},
	LineCmt:  []string{"#"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}, {Quote: '\'', Escape: '\\'}},
	Keywords: "break else for function if in next repeat require library return switch while",
	Types:    "character complex double integer list logical matrix numeric vector data.frame factor",
	Consts:   "NULL Inf NaN NA NA_integer_ NA_real_ NA_character_",
	Bools:    "TRUE FALSE T F",
	Hints:    true,
}

// juliaLang is Julia.
var juliaLang = &Language{
	Name:    "julia",
	Nest:    true,
	Exts:    []string{".jl"},
	LineCmt: []string{"#"},
	Block:   [2]string{"#=", "=#"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\', Repeat: true},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "abstract baremodule begin break catch const continue do else elseif end export finally for function global if import let local macro module quote return struct try using where while in isa mutable primitive type",
	Types:    "Any AnyRef Bool Char Dict Float32 Float64 Int Int8 Int16 Int32 Int64 Nothing String Symbol Tuple Union",
	Consts:   "missing",
	Bools:    "true false",
	Hints:    true,
}

// perlLang is Perl.
var perlLang = &Language{
	Name:     "perl",
	Regex:    true,
	Exts:     []string{".pl", ".pm", ".t"},
	Heredoc:  hdPerl,
	LineCmt:  []string{"#"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}, {Quote: '\'', Escape: '\\'}},
	Vars:     true,
	Keywords: "BEGIN END and cmp continue do else elsif eq for foreach ge gt if last le local lt my ne next no not or our package redo require return sub unless until use while unless",
	Consts:   "undef",
	Hints:    true,
}

// powershellLang is PowerShell.
var powershellLang = &Language{
	Name:     "powershell",
	Exts:     []string{".ps1", ".psm1", ".psd1"},
	LineCmt:  []string{"#"},
	Block:    [2]string{"<#", "#>"},
	Strings:  []StringRule{{Quote: '"', Escape: '`'}, {Quote: '\''}},
	Hyphen:   true,
	NoCase:   true,
	Vars:     true,
	Keywords: "begin break catch class continue data do dynamicparam else elseif end enum exit filter finally for foreach from function if in param process return switch throw trap try until using var while workflow",
	Types:    "bool byte char decimal double float int long object string",
	Consts:   "$null $true $false",
	Hints:    true,
}

// groovyLang is Groovy.
var groovyLang = &Language{
	Name:     "groovy",
	Exts:     []string{".groovy", ".gradle", ".gvy"},
	LineCmt:  []string{"//"},
	DocBlock: true,
	Block:    [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\', Repeat: true},
		{Quote: '\'', Escape: '\\', Repeat: true},
	},
	Keywords: "abstract as assert break case catch class const continue def default do else enum extends final finally for if implements import in instanceof interface native new package private protected public return static strictfp super switch synchronized this throw throws trait transient try var void volatile while yield",
	Types:    "BigDecimal BigInteger Boolean Byte Character Closure Double Float Integer List Long Map Object Set Short String",
	Consts:   "null",
	Bools:    "true false",
	Hints:    true,
}

// hclLang is Terraform and HCL.
var hclLang = &Language{
	Name:     "hcl",
	Hyphen:   true,
	Exts:     []string{".tf", ".tfvars", ".hcl", ".nomad"},
	LineCmt:  []string{"#", "//"},
	Block:    [2]string{"/*", "*/"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	Heredoc:  hdHCL,
	KeySep:   '=',
	Keywords: "count data depends_on dynamic for_each import lifecycle locals module output provider resource terraform variable",
	Consts:   "null",
	Bools:    "true false",
	Hints:    true,
}

// protoLang is Protocol Buffers.
var protoLang = &Language{
	Name:     "proto",
	Exts:     []string{".proto"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	KeySep:   '=',
	Keywords: "edition enum extend extensions import map message oneof option package public repeated required reserved rpc returns service stream syntax weak optional",
	Types:    "bool bytes double fixed32 fixed64 float int32 int64 sfixed32 sfixed64 sint32 sint64 string uint32 uint64",
	Bools:    "true false",
	Hints:    true,
}

// nixLang is Nix.
var nixLang = &Language{
	Name:    "nix",
	Nest:    true,
	Exts:    []string{".nix"},
	LineCmt: []string{"#"},
	Block:   [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		// '' opens the indented string; it spans lines and its own pair
		// introduces the escapes.
		{Quote: '\'', Pair: true, MultiLine: true},
	},
	Keywords: "assert else if in inherit let or rec then with import builtins derivation",
	Consts:   "null",
	Bools:    "true false",
	Hints:    true,
}

// xmlLang is XML, SVG and plists.
var xmlLang = &Language{
	Name:   "xml",
	Exts:   []string{".xml", ".plist", ".svg", ".xsd", ".xsl", ".rss"},
	Markup: true,
	Hyphen: true,
	Block:  [2]string{"<!--", "-->"},
	Strings: []StringRule{
		{Quote: '"'},
		{Quote: '\''},
	},
}

// graphqlLang is GraphQL.
var graphqlLang = &Language{
	Name:     "graphql",
	Exts:     []string{".graphql", ".gql"},
	LineCmt:  []string{"#"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\', Repeat: true}},
	Keywords: "directive enum extend fragment implements input interface mutation on query scalar schema subscription type union",
	Types:    "Boolean Float ID Int String",
	Consts:   "null",
	Bools:    "true false",
	Hints:    true,
}

// asmLang is x86 assembly.
var asmLang = &Language{
	Name:     "asm",
	Exts:     []string{".s", ".asm", ".nasm"},
	LineCmt:  []string{";", "#"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}, {Quote: '\'', Escape: '\\'}},
	Keywords: "add and call cmp dec div idiv imul inc int jmp je jne jg jge jl jle jnz jz lea mov mul neg nop or pop push ret sar sbb shl shr sub syscall test xor",
	Types:    "al ah ax eax rax bl bh bx ebx rbx cl ch cx ecx rcx dl dh dx edx rdx rsi rdi rbp rsp r8 r9 r10 r11 r12 r13 r14 r15 byte dword qword word ptr",
}

// solidityLang is Solidity.
var solidityLang = &Language{
	Name:     "solidity",
	Exts:     []string{".sol"},
	LineCmt:  []string{"//"},
	DocCmt:   []string{"///"},
	DocBlock: true,
	Block:    [2]string{"/*", "*/"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}, {Quote: '\'', Escape: '\\'}},
	Keywords: "abstract address bool break calldata catch constant constructor continue contract delete do else emit enum error event external fallback for function if immutable import indexed interface internal is library mapping memory modifier new override payable pragma private public pure receive return returns storage struct throw try unchecked using view virtual while",
	Types:    "address bool bytes bytes1 bytes32 int int8 int32 int64 int128 int256 string uint uint8 uint32 uint64 uint128 uint256",
	Bools:    "true false",
	Hints:    true,
}

// markdownLang is Markdown.
var markdownLang = &Language{
	Name:    "markdown",
	Exts:    []string{".md", ".markdown", ".mdown"},
	Heading: '#',
	Embed: []Embed{
		{Fence: true},
	},
	Strings: []StringRule{
		{Quote: '`', Repeat: true},
		{Quote: '*'},
		{Quote: '_'},
	},
}

// iniLang is ini, cfg and plain conf files.
var iniLang = &Language{
	Name:    "ini",
	Hyphen:  true,
	Exts:    []string{".ini", ".cfg", ".conf", ".properties"},
	LineCmt: []string{";", "#"},
	Strings: []StringRule{{Quote: '"'}, {Quote: '\''}},
	KeySep:  '=',
	Bools:   "true false yes no on off",
}

// dLang is D. Its /* */ and /+ +/ comments both nest, but a table has one
// block delimiter pair, so /+ +/ is not expressed and falls through as two
// operators. r"..." and r#"..."# reuse the Rust raw-string rule.
var dLang = &Language{
	Name:     "d",
	Nest:     true,
	DocBlock: true,
	Exts:     []string{".d", ".di"},
	LineCmt:  []string{"//"},
	DocCmt:   []string{"///"},
	Block:    [2]string{"/*", "*/"},
	Raw:      []RawKind{RawRust},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
		{Quote: '`', MultiLine: true},
	},
	Keywords: "abstract alias asm assert auto body break case cast catch class const continue debug default delegate delete do else enum export extern final finally for foreach function goto if immutable import in inout interface invariant is lazy macro mixin module new nothrow out override package pragma private protected public pure ref return scope shared static struct super switch synchronized template this throw try typeid typeof union unittest version void volatile while with",
	Types:    "bool byte ubyte short ushort int uint long ulong cent ucent char wchar dchar float double real ifloat idouble ireal cfloat cdouble creal void string wstring dstring",
	Consts:   "null",
	Bools:    "true false",
	Hints:    true,
}

// nimLang is Nim. # ends a line and #[ ]# are its block comments, which nest;
// one block pair per table, so they are the block. The block check runs before
// the line comment check, so "#[" opens the block rather than the comment.
var nimLang = &Language{
	Name:    "nim",
	Nest:    true,
	Exts:    []string{".nim", ".nims", ".nimble"},
	LineCmt: []string{"#"},
	Block:   [2]string{"#[", "]#"},
	Raw:     []RawKind{RawRust},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\', Repeat: true},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "addr and as asm bind block break case cast concept const continue converter defer discard distinct div do elif else end enum except export finally for from func if import in include interface is isnot iterator let macro method mixin mod not notin object of or out proc ptr raise ref return shl shr static template try tuple type using var when while xor yield",
	Types:    "int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 float float32 float64 bool char string cstring range seq array set tuple object ref",
	Consts:   "nil",
	Bools:    "true false",
	Hints:    true,
}

// crystalLang is Crystal. A %w(...) word list is not expressed: it scans as a
// modulo operator and parens.
var crystalLang = &Language{
	Name:    "crystal",
	Exts:    []string{".cr"},
	LineCmt: []string{"#"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "abstract alias annotation as as? asm begin break case class def do else elsif end ensure enum extend for fun if in include lib macro module next of out pointerof private protected require rescue return select self sizeof struct super then type typeof union unless until when while with yield",
	Types:    "Bool Char Float32 Float64 Int8 Int16 Int32 Int64 UInt8 UInt16 UInt32 UInt64 String Symbol Array Hash Tuple Set Range Nil",
	Consts:   "nil",
	Bools:    "true false",
	Hints:    true,
}

// objcLang is Objective-C: C plus its @-directives. The scanner has no prefix
// rule for "@word", so the word half of @interface and friends is coloured and
// the @ itself stays plain. .m is also MATLAB's extension; objective-c keeps
// it, and MATLAB is left out.
var objcLang = &Language{
	Name:     "objc",
	DocBlock: true,
	Preproc:  true,
	Exts:     []string{".m", ".mm"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: cKeywords + " interface implementation end property synthesize protocol selector autoreleasepool class optional required",
	Types:    cTypes + " BOOL id SEL Class instancetype",
	Consts:   "NULL nil Nil",
	Bools:    "YES NO",
	Hints:    true,
}

// cudaLang is CUDA: C++ plus the execution-space qualifiers and the built-in
// launch variables, which is a second row in the same shape cppLang uses.
var cudaLang = &Language{
	Name:     "cuda",
	DocBlock: true,
	Preproc:  true,
	Exts:     []string{".cu", ".cuh"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Raw:      []RawKind{RawCpp},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: cppKeywords + " __global__ __device__ __host__ __shared__ __constant__ __launch_bounds__ __grid_constant__ __managed__ __restrict__ gridDim blockDim blockIdx threadIdx warpSize",
	Types:    cppTypes + " dim3 float2 float3 float4 double2 int2 int3 int4 uint2 uint3 uint4 cudaStream_t cudaEvent_t cudaError_t",
	Consts:   "NULL",
	Bools:    "true false",
	Hints:    true,
}

// fortranLang is Fortran. Keywords are case-insensitive, so the table matches
// an identifier in either case via NoCase. Logical literals are written
// .true. and .false.; the dots are punctuation, so the words list without
// them. A doubled-quote escape inside a literal is not understood.
var fortranLang = &Language{
	Name:    "fortran",
	NoCase:  true,
	Exts:    []string{".f", ".f90", ".f95", ".f03", ".f08", ".for"},
	LineCmt: []string{"!"},
	Strings: []StringRule{
		{Quote: '\''},
		{Quote: '"'},
	},
	Keywords: "allocate allocatable assign block call case character class close common contains continue cycle data deallocate dimension do double else elseif end enddo endif entry equivalence exit external forall format function go if implicit import include inquire integer intent interface intrinsic kind logical module namelist nullify open optional parameter pause pointer precision print private procedure program protected public pure read real recursive result return rewind save select sequence stop subroutine target then to type use where while write",
	Types:    "character complex double integer logical real",
	Bools:    "true false",
}

// latexLang is LaTeX. The scanner has no prefix rule for the backslash that
// opens \command, so commands stay plain and a line gets its colour from the
// % comment and the brace and bracket punctuation, which is LaTeX's real
// structure.
var latexLang = &Language{
	Name:    "latex",
	Exts:    []string{".tex", ".sty", ".cls"},
	LineCmt: []string{"%"},
}

// verilogLang is Verilog and SystemVerilog together: one keyword list, since
// SystemVerilog only adds to Verilog's.
var verilogLang = &Language{
	Name:     "verilog",
	Exts:     []string{".v", ".sv", ".svh", ".vh"},
	LineCmt:  []string{"//"},
	Block:    [2]string{"/*", "*/"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	Literals: LitSized,
	Keywords: "always always_comb always_ff always_latch and assign automatic begin buf case casex casez cmos config deassign default defparam disable else end endcase endfunction endgenerate endmodule endprimitive endspecify endtable endtask event for force forever fork function generate genvar if ifnone include initial inout input instance integer join localparam module nand negedge nmos nor not notif0 notif1 or output parameter pmos posedge primitive pull0 pull1 pulldown pullup rcmos real realtime reg release repeat rnmos rpmos rtran rtranif0 rtranif1 scalared signed small specify specparam strong0 strong1 supply0 supply1 table task time tran tranif0 tranif1 tri tri0 tri1 triand trior trireg unsigned use vectored wait wand weak0 weak1 while wire wor xnor xor",
	Types:    "bit byte chandle const int integer logic longint real shortint shortreal string struct time union void",
	Hints:    true,
}

// awkLang is awk. The built-in record and field variables are conventional
// constants, and $ name is a variable like a shell's.
var awkLang = &Language{
	Name:     "awk",
	Exts:     []string{".awk"},
	LineCmt:  []string{"#"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	Keywords: "BEGIN END if else while for do break continue delete function return next exit print printf getline in nextfile",
	Consts:   "NR NF FS OFS RS ORS ARGC ARGV FILENAME FNR SUBSEP",
	Vars:     true,
	Hints:    true,
}

// tclLang is Tcl. # only opens a comment after a space or at the start of a
// line here, which is close to Tcl's "start of a command" rule.
var tclLang = &Language{
	Name:     "tcl",
	Exts:     []string{".tcl", ".tk", ".exp", ".itcl"},
	LineCmt:  []string{"#"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	Keywords: "proc return global upvar set unset if then else elseif for foreach while switch expr catch error eval source namespace variable uplevel list lappend lindex llength lrange lset array dict incr append string regexp puts package require after file open close format scan subst join split break continue",
	Vars:     true,
	Hints:    true,
}

// batchLang is Windows batch files. REM is a comment only at column zero and
// is case-insensitive, which the scanner cannot say, so only the :: form is a
// comment and REM is left plain.
var batchLang = &Language{
	Name:     "batch",
	Exts:     []string{".bat", ".cmd"},
	LineCmt:  []string{"::"},
	Keywords: "echo set if else for in do goto call exit pause errorlevel exist not defined del copy move mkdir rmdir cd pushd popd shift setlocal endlocal start title cls color type findstr choice timeout",
}

// vimLang is Vim script. Its " is both a comment and the string quote, so the
// marker only counts as a comment as the first non-blank byte of a line:
// `" a comment` is a comment, `echo "hi"` is a string.
var vimLang = &Language{
	Name:         "vim",
	Exts:         []string{".vim"},
	LineCmt:      []string{"\""},
	CommentFirst: true,
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "if else elseif endif for endfor while endwhile function endfunction let set call return try catch finally endtry augroup autocmd command normal execute echo echom map nmap imap vmap unmap source runtime setlocal setglobal syntax highlight nnoremap inoremap vnoremap abbreviate",
	Hints:    true,
}

// lispLang is Scheme, Common Lisp and Emacs Lisp, whose :keywords and lists
// share one shape, so one table serves the three.
var lispLang = &Language{
	Name:     "lisp",
	Atoms:    true,
	Exts:     []string{".scm", ".lisp", ".el"},
	LineCmt:  []string{";"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	Keywords: "define define-syntax syntax-rules lambda let let* letrec if cond else and or not begin do quote quasiquote unquote unquote-splicing set! delay force cons car cdr list reverse append length map apply eval display error defun defmacro defvar defparameter defconstant setq setf progn when unless loop dolist dotimes catch throw block return-from",
	Consts:   "nil",
	Hints:    true,
}

// fsharpLang is F#: OCaml-shaped comments that nest, .NET-adjacent types and
// one triple-quoted string form.
var fsharpLang = &Language{
	Name:    "fsharp",
	Nest:    true,
	Exts:    []string{".fs", ".fsi", ".fsx"},
	LineCmt: []string{"//"},
	Block:   [2]string{"(*", "*)"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\', Repeat: true},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "abstract and as assert begin class default delegate do done downcast downto elif else end exception extern false finally for fun function global if in inherit inline interface internal lazy let match member module mutable namespace new not of open or override private public rec return static struct then to true try type upcast use val void when while with yield",
	Types:    "int int8 int16 int32 int64 uint8 uint16 uint32 uint64 float float32 decimal bool byte char string unit list array seq option obj Map Set Result",
	Consts:   "null",
	Bools:    "true false",
	Hints:    true,
}

// purescriptLang is PureScript, whose block comments nest like Haskell's.
var purescriptLang = &Language{
	Name:     "purescript",
	Nest:     true,
	Exts:     []string{".purs"},
	LineCmt:  []string{"--"},
	Block:    [2]string{"{-", "-}"},
	Strings:  []StringRule{{Quote: '"', Escape: '\\'}},
	Keywords: "case class data default deriving do else foreign if import in infix infixl infixr instance let module newtype of then type where as forall",
	Types:    "Int Number String Boolean Char Unit Array List Maybe Either Map Set Tuple Record",
	Consts:   "Nothing",
	Bools:    "true false",
	Hints:    true,
}

// rakuLang is Raku (formerly Perl 6).
var rakuLang = &Language{
	Name:    "raku",
	Exts:    []string{".raku", ".rakumod", ".p6", ".pl6", ".pm6"},
	LineCmt: []string{"#"},
	Strings: []StringRule{
		{Quote: '"', Escape: '\\'},
		{Quote: '\'', Escape: '\\'},
	},
	Keywords: "use need module class role sub method my our has is does but if elsif else unless for loop while until given when default where with orwith without return state constant enum grammar token regex rule proto multi package begin react whenever async await start supply emit done say print put warn fail die exit open close slurp spurt CATCH CONTROL unit",
	Types:    "Int Int8 Int16 Int32 Int64 Num Rat Bool Str Array Hash List Map Set Pair Junction Complex",
	Consts:   "Nil",
	Bools:    "True False",
	Vars:     true,
	Hints:    true,
}

// nginxLang is an nginx configuration: a flat file of directives.
var nginxLang = &Language{
	Name:     "nginx",
	Files:    []string{"nginx.conf"},
	LineCmt:  []string{"#"},
	Keywords: "server listen root location proxy_pass upstream index try_files worker_processes events http stream include error_page rewrite return set if add_header access_log error_log keepalive_timeout sendfile tcp_nopush client_max_body_size ssl_certificate ssl_certificate_key server_name resolver map limit_req zone",
}

// languages is every table, in detection order.
var languages = []*Language{
	goLang, rustLang, pythonLang, javascriptLang, typescriptLang, typescriptJSX,
	cLang, cppLang, javaLang, csharpLang, rubyLang, phpLang, phpPageLang,
	shellLang, luaLang, sqlLang, jsonLang, yamlLang, tomlLang,
	htmlLang, vueLang, cssLang, xmlLang, markdownLang,
	kotlinLang, swiftLang, scalaLang, dartLang, groovyLang,
	elixirLang, erlangLang, haskellLang, elmLang, ocamlLang, clojureLang,
	zigLang, rLang, juliaLang, perlLang, powershellLang,
	asmLang, solidityLang, protoLang, graphqlLang,
	hclLang, nixLang, iniLang,
	dLang, nimLang, crystalLang, objcLang, cudaLang, fortranLang, latexLang,
	verilogLang, awkLang, tclLang, batchLang, vimLang, lispLang, fsharpLang,
	purescriptLang, rakuLang, nginxLang,
}

// fenceTag maps a fenced code block's info tag to a table. An unknown or
// missing tag returns nil, which leaves the body plain.
func fenceTag(tag string) *Language {
	switch tag {
	case "go", "golang":
		return goLang
	case "rust":
		return rustLang
	case "python", "py":
		return pythonLang
	case "js", "javascript", "jsx":
		return javascriptLang
	case "ts", "typescript":
		return typescriptLang
	case "tsx":
		return typescriptJSX
	case "sh", "bash", "shell", "zsh":
		return shellLang
	case "json":
		return jsonLang
	case "yaml", "yml":
		return yamlLang
	case "toml":
		return tomlLang
	case "sql":
		return sqlLang
	case "html":
		return htmlLang
	case "css", "scss":
		return cssLang
	case "c":
		return cLang
	case "cpp", "c++", "hpp":
		return cppLang
	case "java":
		return javaLang
	case "kotlin":
		return kotlinLang
	case "swift":
		return swiftLang
	case "dart":
		return dartLang
	case "ruby":
		return rubyLang
	case "php":
		return phpLang
	case "lua":
		return luaLang
	case "haskell":
		return haskellLang
	case "elixir":
		return elixirLang
	case "erlang":
		return erlangLang
	case "ocaml":
		return ocamlLang
	case "clojure":
		return clojureLang
	case "zig":
		return zigLang
	case "r":
		return rLang
	case "julia":
		return juliaLang
	case "perl":
		return perlLang
	case "powershell", "ps1":
		return powershellLang
	case "hcl", "terraform":
		return hclLang
	case "proto":
		return protoLang
	case "graphql":
		return graphqlLang
	case "nix":
		return nixLang
	case "solidity":
		return solidityLang
	case "asm":
		return asmLang
	case "ini":
		return iniLang
	case "xml":
		return xmlLang
	case "markdown", "md":
		return markdownLang
	case "d":
		return dLang
	case "nim":
		return nimLang
	case "crystal", "cr":
		return crystalLang
	case "objc", "objective-c":
		return objcLang
	case "cuda":
		return cudaLang
	case "fortran", "f90":
		return fortranLang
	case "latex", "tex":
		return latexLang
	case "verilog", "systemverilog":
		return verilogLang
	case "awk":
		return awkLang
	case "tcl":
		return tclLang
	case "batch", "bat":
		return batchLang
	case "vim", "vimscript":
		return vimLang
	case "lisp", "scheme", "cl", "elisp":
		return lispLang
	case "fsharp", "f#":
		return fsharpLang
	case "purescript", "purs":
		return purescriptLang
	case "raku":
		return rakuLang
	case "nginx":
		return nginxLang
	}
	return nil
}

// Detect picks the language of a file from its name, then its extension, then
// the interpreter line of a script. It returns nil when it does not know, which
// leaves the line exactly as the diff already colours it.
func Detect(path, firstLine string) *Language {
	base := filepath.Base(path)
	for _, l := range languages {
		for _, name := range l.Files {
			if name == base {
				return l
			}
		}
	}
	if ext := strings.ToLower(filepath.Ext(base)); ext != "" {
		for _, l := range languages {
			for _, e := range l.Exts {
				if e == ext {
					return l
				}
			}
		}
	}
	return shebang(firstLine)
}

// shebang maps an interpreter line to a language.
func shebang(line string) *Language {
	if !strings.HasPrefix(line, "#!") {
		return nil
	}
	for _, field := range strings.Fields(line) {
		field = filepath.Base(field)
		switch {
		case strings.HasPrefix(field, "python"):
			return pythonLang
		case field == "node" || field == "deno" || field == "bun":
			return javascriptLang
		case field == "ruby":
			return rubyLang
		case field == "lua":
			return luaLang
		case field == "sh" || field == "bash" || field == "zsh" || field == "dash" || strings.HasPrefix(field, "bash"):
			return shellLang
		}
	}
	return nil
}

// Languages is the number of languages a file can be detected as.
func Languages() int { return len(languages) }

// Words is the number of reserved words in every table, which the size test
// watches so support cannot creep into bloat unnoticed.
func Words() int {
	n := 0
	for _, l := range languages {
		n += len(strings.Fields(l.Keywords)) + len(strings.Fields(l.Types)) + len(strings.Fields(l.Consts))
	}
	return n
}
