import { afterEach, vi } from "vitest";
import { cleanup } from "@testing-library/react";
import "@testing-library/jest-dom/vitest";

afterEach(() => {
  cleanup();
  localStorage.clear();
  sessionStorage.clear();
  window.location.hash = "";
  vi.restoreAllMocks();
});
