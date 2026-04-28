// Package endpoints owns daemon-side operator queries and writes for endpoint
// intent.
//
// It exposes resource-shaped endpoint control use cases over the repository
// port. Transport contracts such as HTTP routes belong in inbound adapters;
// persistence details belong behind the repository.
package endpoints
