import type { Metadata } from "next";
import type { ReactNode } from "react";
import "./globals.css";

// Metadata controls the browser tab title and gives the dashboard a meaningful
// identity before we add navigation and multiple pages.
export const metadata: Metadata = {
  title: "MoneyPlant Dashboard",
  description: "Market and macroeconomic data dashboard"
};

// The root layout surrounds every App Router page. Keeping the body styling and
// global stylesheet import here ensures future dashboard routes share the same
// baseline without repeating setup code.
export default function RootLayout({
  children
}: Readonly<{
  children: ReactNode;
}>) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
