// MIT License
//
// Copyright (c) 2019 Stefan Wichmann
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
package main

import (
	"time"

	log "github.com/Sirupsen/logrus"
	hue "github.com/stefanwichmann/go.hue"
)

// Light represents a light kelvin can automate in your system.
type Light struct {
	ID               int        `json:"id"`
	Name             string     `json:"name"`
	HueLight         HueLight   `json:"-"`
	TargetLightState LightState `json:"targetLightState,omitempty"`
	Scheduled        bool       `json:"scheduled"`
	Reachable        bool       `json:"reachable"`
	On               bool       `json:"on"`
	Tracking         bool       `json:"-"`
	Automatic        bool       `json:"automatic"`
	Schedule         Schedule   `json:"-"`
	Interval         Interval   `json:"interval"`
	Appearance       time.Time  `json:"-"`
}

func (light *Light) updateCurrentLightState(attr hue.LightAttributes) error {
	light.HueLight.updateCurrentLightState(attr)
	light.Reachable = light.HueLight.Reachable
	light.On = light.HueLight.On
	return nil
}

func (light *Light) update() (bool, error) {
	// Is the light associated to any schedule?
	if !light.Scheduled {
		return false, nil
	}

	// If the light is not reachable anymore clean up
	if !light.Reachable {
		if light.Tracking {
			log.Printf("💡 Light %s - Light is no longer reachable. Clearing state...", light.Name)
			light.Tracking = false
			light.Automatic = false
			return false, nil
		}

		// Ignore light because we are not tracking it.
		return false, nil
	}

	// If the light was turned off clean up
	if !light.On {
		if light.Tracking {
			log.Printf("💡 Light %s - Light was turned off. Clearing state...", light.Name)
			light.Tracking = false
			light.Automatic = false
			return false, nil
		}

		// Ignore light because we are not tracking it.
		return false, nil
	}

	// Did the light just appear?
	if !light.Tracking {
		log.Printf("💡 Light %s - Light just appeared.", light.Name)
		light.Tracking = true
		light.Appearance = time.Now()

		// Should we auto-enable Kelvin?
		if light.Schedule.enableWhenLightsAppear {
			log.Printf("💡 Light %s - Initializing state to %vK at %v%% brightness.", light.Name, light.TargetLightState.ColorTemperature, light.TargetLightState.Brightness)

			err := light.HueLight.setLightState(light.TargetLightState.ColorTemperature, light.TargetLightState.Brightness)
			if err != nil {
				return false, err
			}

			light.Automatic = true
			log.Debugf("💡 Light %s - Light was initialized to %vK at %v%% brightness", light.Name, light.TargetLightState.ColorTemperature, light.TargetLightState.Brightness)

			return true, nil
		}
	}

	// Ignore light if it was changed manually
	if !light.Automatic {
		// return if we should ignore color temperature and brightness
		if light.TargetLightState.ColorTemperature == -1 && light.TargetLightState.Brightness == -1 {
			return false, nil
		}

		// if status == scene state --> Activate Kelvin
		if light.HueLight.hasState(light.TargetLightState.ColorTemperature, light.TargetLightState.Brightness) {
			log.Printf("💡 Light %s - Detected matching target state. Activating Kelvin...", light.Name)
			light.Automatic = true

			// set correct target lightstate on HueLight
			err := light.HueLight.setLightState(light.TargetLightState.ColorTemperature, light.TargetLightState.Brightness)
			if err != nil {
				return false, err
			}
		}
		return true, nil
	}

	// Did the user manually change the light state?
	if light.HueLight.hasChanged() {
		if log.GetLevel() == log.DebugLevel {
			log.Debugf("💡 Light %s - Light state has been changed manually after %v: %+v", light.Name, time.Since(light.Appearance), light.HueLight)
		} else {
			log.Printf("💡 Light %s - Light state has been changed manually. Disabling Kelvin...", light.Name)
		}
		light.Automatic = false
		return false, nil
	}

	// Update of lightstate needed?
	if light.HueLight.hasState(light.TargetLightState.ColorTemperature, light.TargetLightState.Brightness) {
		return false, nil
	}

	// Light is turned on and in automatic state. Set target lightstate.
	err := light.HueLight.setLightState(light.TargetLightState.ColorTemperature, light.TargetLightState.Brightness)
	if err != nil {
		return false, err
	}

	log.Printf("💡 Light %s - Updated light state to %vK at %v%% brightness", light.Name, light.TargetLightState.ColorTemperature, light.TargetLightState.Brightness)
	return true, nil
}

func (light *Light) updateSchedule(schedule Schedule) {
	light.Schedule = schedule
	light.Scheduled = true
	log.Printf("💡 Light %s - Activating schedule for %v (Sunrise: %v, Sunset: %v)", light.Name, light.Schedule.endOfDay.Format("Jan 2 2006"), light.Schedule.sunrise.Time.Format("15:04"), light.Schedule.sunset.Time.Format("15:04"))
	light.updateInterval()
}

func (light *Light) updateInterval() {
	if !light.Scheduled {
		log.Debugf("💡 Light %s - Light is not associated to any schedule. No interval to update...", light.Name)
		return
	}

	newInterval, err := light.Schedule.currentInterval(time.Now())
	if err != nil {
		log.Printf("💡 Light %s - Light has no active interval. Ignoring...", light.Name)
		light.Interval = newInterval // Assign empty interval
		return
	}
	if newInterval != light.Interval {
		light.Interval = newInterval
		log.Printf("💡 Light %s - Activating interval %v - %v", light.Name, light.Interval.Start.Time.Format("15:04"), light.Interval.End.Time.Format("15:04"))
	}
}

func (light *Light) updateTargetLightState() {
	if !light.Scheduled {
		log.Debugf("💡 Light %s - Light is not associated to any schedule. No target light state to update...", light.Name)
		return
	}

	// Calculate the target lightstate from the interval
	newLightState := light.Interval.calculateLightStateInInterval(time.Now())

	// Did the target light state change?
	if newLightState.equals(light.TargetLightState) {
		return
	}

	// First initialization of the TargetLightState?
	if light.TargetLightState.ColorTemperature == 0 && light.TargetLightState.Brightness == 0 {
		log.Debugf("💡 Light %s - Initialized target light state for the interval %v - %v to %+v", light.Name, light.Interval.Start.Time.Format("15:04"), light.Interval.End.Time.Format("15:04"), newLightState)
	} else {
		log.Debugf("💡 Light %s - Updated target light state for the interval %v - %v to %+v", light.Name, light.Interval.Start.Time.Format("15:04"), light.Interval.End.Time.Format("15:04"), newLightState)
	}

	light.TargetLightState = newLightState
}
