// fsharp sum
let rec fact n =
    if n < 2 then 1
    else n * fact (n - 1)

(* nested (* comment *) *)
let s = """triple
quoted"""
printfn "%d" (fact 10)
