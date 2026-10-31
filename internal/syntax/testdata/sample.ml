(* A bounded cache of recent keys. *)

type t = {
  capacity : int;
  mutable slots : string list;
}

let create ?(capacity = 16) () = { capacity; slots = [] }

let push cache key =
  let slots = key :: cache.slots in
  cache.slots <- List.filteri (fun i _ -> i < cache.capacity) slots

let contains cache key = List.mem key cache.slots

let report cache =
  Printf.sprintf {|{"kind": "cache", "size": %d}|} (List.length cache.slots)

(* Block comments nest: (* inner *) is still a comment. *)
let mask = 0xff land 0b1111
let delim = ';'
let ratio = 1.5e3

let () =
  let cache = create ~capacity:16 () in
  push cache "one";
  push cache "two";
  print_endline (report cache)
