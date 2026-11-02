export function formatMoney(paisa: number) {
  return new Intl.NumberFormat("en-PK", {
    style: "currency",
    currency: "PKR",
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(paisa / 100);
}

export function formatDistance(meters: number) {
  const kilometers = meters / 1000;
  return `${new Intl.NumberFormat("en-PK", { maximumFractionDigits: 1 }).format(kilometers)} km`;
}

export function formatDate(dateTime: string) {
  return new Intl.DateTimeFormat("en-PK", {
    day: "numeric",
    month: "short",
    year: "numeric",
    timeZone: "UTC",
  }).format(new Date(dateTime));
}

export function formatCategory(category: "economy" | "comfort") {
  return category === "economy" ? "Economy" : "Comfort";
}

export function shortRideId(id: string) {
  return id.length > 16 ? `${id.slice(0, 8)}…${id.slice(-6)}` : id;
}
