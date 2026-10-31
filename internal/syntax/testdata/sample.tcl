# tcl procedure
proc fib {n} {
    if {$n < 2} {
        return $n
    }
    expr {[fib [expr {$n - 1}]] + [fib [expr {$n - 2}]]}
}

puts "fib 10 = [fib 10]"
