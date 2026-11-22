import { defineCollection } from "astro:content";
import { glob } from "astro/loaders";

// The repository's docs/*.md are the source of the docs pages. Entry ids are
// their paths without .md, such as "configuration" or "providers/clerk".
export const collections = {
  docs: defineCollection({
    loader: glob({
      pattern: "**/*.md",
      base: "../docs",
      generateId: ({ entry }) => entry.replace(/\.md$/, ""),
    }),
  }),
};
