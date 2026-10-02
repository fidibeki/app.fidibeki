# Editorial photography

These photographs illustrate work, conversation, and place. They do not depict
Fidibeki staff or customers and are not endorsements. The Aveiro photograph is
captioned as the city, not an office.

All five photographs were downloaded from Unsplash on 2 October 2026 under the
[Unsplash License](https://unsplash.com/license), which permits free commercial
use. Each is stored locally as a 640px and a 1200px wide WebP. The browser chooses
the appropriate file through `srcset`; no visitor request goes to Unsplash.

| File prefix     | Photographer and original                                                                                               | Used for                                                   |
| --------------- | ----------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------- |
| `collaboration` | [Headway](https://unsplash.com/photos/person-gesturing-during-meeting-with-laptop-5QgIuuBxKwM)                          | Features introduction, issue tracking, in-product research |
| `laptop`        | [Glenn Carstens-Peters](https://unsplash.com/photos/person-using-macbook-pro-npxXWgQ33ZQ)                               | Feedback widget, bug reports                               |
| `notebook`      | [David Iskander](https://unsplash.com/photos/person-opening-notebook-on-brown-wooden-table-8hFiT80X-6o)                 | Feature requests                                           |
| `conversation`  | [Priscilla Du Preez](https://unsplash.com/photos/a-couple-of-people-sitting-at-a-table-with-cups-of-coffee-3gAiajAfjXI) | Reporter loop, customer support                            |
| `aveiro`        | [Ricardo Resende](https://unsplash.com/photos/a-couple-of-boats-in-a-canal-5F4TOnZJDIQ)                                 | Contact, captioned "Aveiro, Portugal"                      |

Alt text, intrinsic dimensions, focal positions, and original source URLs live
in `marketing/data/photography.ts`. `EditorialPhoto.vue` reserves the image frame
before loading and uses lazy loading except for page introductions.
