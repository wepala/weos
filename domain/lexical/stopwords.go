// Copyright (C) 2026 Wepala, LLC
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package lexical

import "strings"

// stopWords is the one stop list of an any-word search, on both engines: the
// English words too common to say what a question is about. It is applied to
// the query only; the index holds every word, and an all-words search drops
// none.
//
// The first block is the support hub's list (services/weos-support
// support/answers.go stopWords), word for word, so the hub and core agree on
// what a content word is. The second block adds what a contraction leaves
// behind once its apostrophe splits it, and the modals the hub's list lacks:
// "can't" is "can" + "t", "doesn't" is "doesn" + "t", "you're" is "you" + "re".
// Every part is a stop word, so a contraction is dropped whole. A query word
// "can" would match every "you can …" text as strongly as every "can't …"
// one, which reads as the opposite meaning; lexical search does not model
// negation, so the content words carry the match. The third block is the
// greetings and thanks a person wraps a support question in ("hi, please
// help, thanks"): they say nothing about what the question is about, and
// each one kept would spend one of the MaxContentWords on nothing.
var stopWords = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`a about after again all am an and any are as at be because been
		before being but by can could did do does doing don for from further had has have having he her
		here hers him his how i if in into is it its itself just me more most my myself no nor not now
		of off on once only or other our ours out over own same she should so some such than that the
		their theirs them then there these they this those through to too under until up very was we
		were what when where which while who whom why will with would you your yours yourself
		ll ve re im ive dont doesnt didnt cant isnt wont

		cannot may might must shall t s d m
		doesn didn isn aren wasn weren hasn haven hadn won wouldn shouldn couldn mustn shan needn

		please thanks thank hello hi hey`) {
		m[w] = true
	}
	return m
}()
