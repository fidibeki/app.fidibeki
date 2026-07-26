import { defineTestimonialElements } from './wall'

// The loader is the one line a customer pastes. It captures its own origin so
// <fidibeki-wall> can call the right API host without the customer configuring
// anything, then registers the elements.
//
// document.currentScript is only readable while the script is executing, so it
// has to be captured here rather than inside the element.
const current = document.currentScript as HTMLScriptElement | null
defineTestimonialElements(current?.src ? new URL(current.src).origin : '')
