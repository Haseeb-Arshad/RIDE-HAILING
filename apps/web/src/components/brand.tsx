import Link from "next/link";
import { Route } from "lucide-react";

export function Brand() {
  return (
    <Link className="brand" href="/" aria-label="Ride Lahore home">
      <span className="brand-mark"><Route size={23} strokeWidth={2} aria-hidden="true" /></span>
      <span>Ride Lahore<span className="brand-caption">YOUR CITY. YOUR WAY.</span></span>
    </Link>
  );
}
