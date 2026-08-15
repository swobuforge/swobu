// Package novita binds Novita AI's managed and deployment Chat surface to the
// shared OpenAI-family runtime. It owns only the observed reasoning_details
// carrier and its exact provider-scoped replay; base URLs, models, transport,
// and protocol grammar remain shared facts.
package novita
