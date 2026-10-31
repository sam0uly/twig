# raku fib
sub fib($n) {
    if $n < 2 { $n } else { fib($n - 1) + fib($n - 2) }
}

my $n = 10;
say "fib 10 = " ~ fib($n);
