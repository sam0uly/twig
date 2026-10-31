// fibonacci in D
import std.stdio;

auto fib(int n) {
    if (n < 2) return n;
    return fib(n - 1) + fib(n - 2);
}

void main() {
    auto s = `raw "string"`;
    auto r = r"raw too";
    /* a nested /* block */ comment */
    writeln("fib 10 = ", fib(10));
}
