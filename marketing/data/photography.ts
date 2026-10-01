/** Editorial photographs illustrate situations, not Fidibeki staff or customers. */
export interface EditorialImage {
  name: string
  alt: string
  width: number
  height: number
  position?: string
  photographer: string
  source: string
}

export const photography = {
  collaboration: {
    name: 'collaboration',
    alt: 'A conversation around a work table, with a laptop and an open notebook.',
    width: 1200,
    height: 800,
    photographer: 'Headway',
    source:
      'https://unsplash.com/photos/person-gesturing-during-meeting-with-laptop-5QgIuuBxKwM',
  },
  laptop: {
    name: 'laptop',
    alt: 'Hands working at a laptop beside a sunlit window.',
    width: 1200,
    height: 798,
    photographer: 'Glenn Carstens-Peters',
    source: 'https://unsplash.com/photos/person-using-macbook-pro-npxXWgQ33ZQ',
  },
  notebook: {
    name: 'notebook',
    alt: 'A person with an open book and a pen at a wooden café table.',
    width: 1200,
    height: 800,
    position: '50% 42%',
    photographer: 'David Iskander',
    source:
      'https://unsplash.com/photos/person-opening-notebook-on-brown-wooden-table-8hFiT80X-6o',
  },
  conversation: {
    name: 'conversation',
    alt: 'Two people holding cups of coffee across a small table.',
    width: 1200,
    height: 1800,
    position: '50% 52%',
    photographer: 'Priscilla Du Preez',
    source:
      'https://unsplash.com/photos/a-couple-of-people-sitting-at-a-table-with-cups-of-coffee-3gAiajAfjXI',
  },
  aveiro: {
    name: 'aveiro',
    alt: 'Traditional boats on a canal beside sunlit buildings in Aveiro, Portugal.',
    width: 1200,
    height: 1554,
    position: '50% 54%',
    photographer: 'Ricardo Resende',
    source:
      'https://unsplash.com/photos/a-couple-of-boats-in-a-canal-5F4TOnZJDIQ',
  },
} satisfies Record<string, EditorialImage>

export const useCasePhotography: Record<string, EditorialImage> = {
  'bug-reports': photography.laptop,
  'feature-requests': photography.notebook,
  'customer-support': photography.conversation,
  'in-product-research': photography.collaboration,
}

export const featurePhotography: Partial<Record<string, EditorialImage>> = {
  'feedback-widget': photography.laptop,
  'issue-tracking': photography.collaboration,
  'reporter-loop': photography.conversation,
}
