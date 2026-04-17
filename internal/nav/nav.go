// Package nav models the navigation stack: a LIFO of frames representing
// "where the user currently is" in the hierarchy. Push when drilling, pop on
// back.
package nav

import "github.com/tesserix/cloudnav/internal/provider"

type Frame struct {
	Title  string
	Parent *provider.Node
	Nodes  []provider.Node
}

type Stack struct {
	frames []Frame
}

func (s *Stack) Push(f Frame)    { s.frames = append(s.frames, f) }
func (s *Stack) Depth() int      { return len(s.frames) }
func (s *Stack) Frames() []Frame { return s.frames }

func (s *Stack) Pop() (Frame, bool) {
	if len(s.frames) == 0 {
		return Frame{}, false
	}
	top := s.frames[len(s.frames)-1]
	s.frames = s.frames[:len(s.frames)-1]
	return top, true
}

func (s *Stack) Top() *Frame {
	if len(s.frames) == 0 {
		return nil
	}
	return &s.frames[len(s.frames)-1]
}

func (s *Stack) Breadcrumbs() []string {
	out := make([]string, 0, len(s.frames))
	for _, f := range s.frames {
		out = append(out, f.Title)
	}
	return out
}
