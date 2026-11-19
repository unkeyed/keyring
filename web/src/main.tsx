import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createBrowserRouter, Navigate, RouterProvider } from "react-router";
import { ApiError } from "./api";
import { Layout, NotFound } from "./Layout";
import { Login } from "./pages/Login";
import { AcceptInvite } from "./pages/AcceptInvite";
import { Contacts } from "./pages/Contacts";
import { ContactPage } from "./pages/ContactPage";
import "./index.css";

const router = createBrowserRouter([
  { path: "/login", element: <Login /> },
  { path: "/invite/:token", element: <AcceptInvite /> },
  {
    element: <Layout />,
    children: [
      { index: true, element: <Navigate to="/contacts" replace /> },
      { path: "contacts", element: <Contacts /> },
      { path: "contacts/:id", element: <ContactPage /> },
      { path: "*", element: <NotFound /> },
    ],
  },
]);

/** A 401 anywhere means the session ended: go to login and come back after. */
function onError(err: unknown) {
  if (!(err instanceof ApiError) || err.status !== 401) return;
  const { pathname, search } = router.state.location;
  if (pathname === "/login" || pathname.startsWith("/invite/")) return;
  queryClient.clear();
  void router.navigate(`/login?next=${encodeURIComponent(pathname + search)}`, { replace: true });
}

const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError }),
  mutationCache: new MutationCache({ onError }),
  defaultOptions: {
    queries: {
      staleTime: 10_000,
      retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  </StrictMode>,
);
