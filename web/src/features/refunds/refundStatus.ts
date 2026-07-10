export function refundStatusLabel(status: string) {
  switch (status) {
    case "REQUESTED":
      return "Requested";
    case "COMPLETED":
      return "Completed";
    case "FAILED":
      return "Failed";
    case "NONE":
      return "No refund";
    default:
      return status || "Unknown";
  }
}

export function refundStatusClass(status: string) {
  switch (status) {
    case "COMPLETED":
      return "green";
    case "REQUESTED":
      return "amber";
    case "FAILED":
      return "danger";
    default:
      return "slate";
  }
}
