import type { Metadata } from "next";
import { AuthProvider } from "@/lib/auth-provider";
import "./globals.css";

export const metadata: Metadata = {
  title: "Ride Lahore",
  description: "A Lahore ride-booking demo.",
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="en">
      <body>
        <AuthProvider>{children}</AuthProvider>
      </body>
    </html>
  );
}
