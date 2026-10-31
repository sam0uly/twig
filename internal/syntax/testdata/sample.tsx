import { useState } from "react";

type Props = { items: string[] };

/** A documented component. */
export function List({ items }: Props) {
  const [open, setOpen] = useState(false);
  const smaller = items.length < 3;

  return (
    <section className="list" aria-open={open}>
      <button onClick={() => setOpen(!open)}>
        {open ? "hide" : "show"}
      </button>
      {open && smaller && <p className="hint">a &lt; b</p>}
      {items.map((item) => (
        <li key={item}>{item}</li>
      ))}
    </section>
  );
}
